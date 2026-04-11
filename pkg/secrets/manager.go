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

// Package secrets provides secret storage and retrieval functionality for QuicBaaS.
package secrets

import (
	"context"
	stderrors "errors"
	"sync"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// Manager provides a unified interface for secrets management operations.
// It wraps a backend and implements the interfaces.SecretsManager interface.
type Manager struct {
	backend Backend
	logger  interfaces.Logger

	// locker is the optional distributed lock service used by
	// EnsureSecret to serialize concurrent generate-or-read critical
	// sections across processes and cluster nodes. When the Manager is
	// configured with a Locker (typical for production), EnsureSecret
	// acquires a per-path lock via lock.Service.TryLock before reading
	// or generating a secret. This works identically on standalone
	// Pebble backends and Raft-replicated clusters because the lock
	// service composes only generic TransactionServicer / LeaseServicer
	// / MVCCKVServicer primitives that both backends implement.
	//
	// When locker is nil, EnsureSecret falls back to a PROCESS-LOCAL
	// mutex (ensureMu) keyed by path. That is ONLY safe in single-
	// process embedded test deployments. Cluster setups MUST pass a
	// non-nil locker via ManagerOptions.Locker or EnsureSecret calls
	// will race between replicas.
	locker qrdbsdk.Locker

	// ensureMu protects the process-local fallback path inside
	// EnsureSecret. Keyed by secret path. Only used when locker == nil.
	ensureMu sync.Map
}

// ManagerOptions configures the secrets Manager.
type ManagerOptions struct {
	// Backend is the underlying secrets backend (required).
	Backend Backend

	// Logger for operation logging (optional).
	Logger interfaces.Logger

	// Locker is the distributed lock service used by EnsureSecret to
	// prevent concurrent generation of the same secret across processes
	// and cluster nodes. Strongly recommended in production. May be nil
	// in single-process embedded deployments (e.g. unit tests); see the
	// Manager.locker field docs for the consequences.
	Locker qrdbsdk.Locker
}

// NewManager creates a new secrets Manager with the specified options.
func NewManager(opts *ManagerOptions) (*Manager, error) {
	if opts == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.NewManager",
			Err:  stderrors.New("options cannot be nil"),
		}
	}

	if opts.Backend == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.NewManager",
			Err:  stderrors.New("backend cannot be nil"),
		}
	}

	m := &Manager{
		backend: opts.Backend,
		logger:  opts.Logger,
		locker:  opts.Locker,
	}

	if m.logger != nil {
		m.logger.WithField("backend", m.backend.Type()).Info("Secrets Manager initialized")
	}

	return m, nil
}

// ensureLockTTL is the TTL of the per-path lock held while EnsureSecret
// is generating and storing a new secret. Generate+Store is a few
// milliseconds at most; 10s is a safe upper bound that covers slow
// Raft commit latency or an unusually long backend store.
const ensureLockTTL = 10 * time.Second

// Store saves a secret at the specified path.
func (m *Manager) Store(ctx context.Context, path string, data map[string][]byte, metadata map[string]string) error {
	if path == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Manager.Store",
			Err:  ErrInvalidPath,
		}
	}

	if len(data) == 0 {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Manager.Store",
			Err:  ErrEmptyData,
		}
	}

	if ctx.Err() != nil {
		return &errors.QBError{
			Code: errors.ErrTimeout,
			Op:   "Manager.Store",
			Err:  ctx.Err(),
		}
	}

	if err := m.backend.Store(ctx, path, data, metadata); err != nil {
		return &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "Manager.Store",
			Err:  err,
		}
	}

	if m.logger != nil {
		m.logger.WithField("path", path).Info("Secret stored")
	}

	return nil
}

// BackendWithOptions defines backends that support encryption options.
type BackendWithOptions interface {
	StoreWithOptions(ctx context.Context, path string, data map[string][]byte, metadata map[string]string, opts *interfaces.StoreOptions) error
}

// StoreWithOptions saves a secret with custom encryption options.
// If opts.TenantID is empty, it will be auto-populated from the context.
func (m *Manager) StoreWithOptions(ctx context.Context, path string, data map[string][]byte, metadata map[string]string, opts *interfaces.StoreOptions) error {
	if path == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Manager.StoreWithOptions",
			Err:  ErrInvalidPath,
		}
	}

	if len(data) == 0 {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Manager.StoreWithOptions",
			Err:  ErrEmptyData,
		}
	}

	if ctx.Err() != nil {
		return &errors.QBError{
			Code: errors.ErrTimeout,
			Op:   "Manager.StoreWithOptions",
			Err:  ctx.Err(),
		}
	}

	// Auto-populate TenantID from context if not explicitly set
	opts = m.enrichOptionsFromContext(ctx, opts)

	// Check if backend supports StoreWithOptions
	if backendWithOpts, ok := m.backend.(BackendWithOptions); ok {
		// Backend supports options, use it
		if err := backendWithOpts.StoreWithOptions(ctx, path, data, metadata, opts); err != nil {
			return &errors.QBError{
				Code: errors.ErrInternal,
				Op:   "Manager.StoreWithOptions",
				Err:  err,
			}
		}

		// Log with options if logger exists
		if m.logger != nil {
			fields := map[string]interface{}{
				"path": path,
			}
			if opts != nil {
				if opts.TenantID != "" {
					fields["tenant_id"] = opts.TenantID
				}
				if opts.KeyIdentifier != "" {
					fields["key_identifier"] = opts.KeyIdentifier
				}
				if opts.Algorithm != "" {
					fields["algorithm"] = opts.Algorithm
				}
			}
			m.logger.WithFields(fields).Info("Secret stored with options")
		}

		return nil
	}

	// Fallback to regular Store if backend doesn't support options
	if m.logger != nil {
		m.logger.Debug("Backend does not support StoreWithOptions, using fallback Store")
	}

	if err := m.backend.Store(ctx, path, data, metadata); err != nil {
		return &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "Manager.StoreWithOptions",
			Err:  err,
		}
	}

	if m.logger != nil {
		m.logger.WithField("path", path).Info("Secret stored (options not supported by backend)")
	}

	return nil
}

// Get retrieves a secret from the specified path.
func (m *Manager) Get(ctx context.Context, path string) (map[string][]byte, map[string]string, error) {
	if path == "" {
		return nil, nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Manager.Get",
			Err:  ErrInvalidPath,
		}
	}

	if ctx.Err() != nil {
		return nil, nil, &errors.QBError{
			Code: errors.ErrTimeout,
			Op:   "Manager.Get",
			Err:  ctx.Err(),
		}
	}

	data, metadata, err := m.backend.Get(ctx, path)
	if err != nil {
		if stderrors.Is(err, ErrSecretNotFound) {
			return nil, nil, &errors.QBError{
				Code: errors.ErrNotFound,
				Op:   "Manager.Get",
				Err:  err,
			}
		}
		if stderrors.Is(err, ErrSecretDeleted) {
			return nil, nil, &errors.QBError{
				Code: errors.ErrNotFound,
				Op:   "Manager.Get",
				Err:  err,
			}
		}
		return nil, nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "Manager.Get",
			Err:  err,
		}
	}

	return data, metadata, nil
}

// GetVersion retrieves a specific version of a secret.
func (m *Manager) GetVersion(ctx context.Context, path string, version int) (map[string][]byte, map[string]string, error) {
	if path == "" {
		return nil, nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Manager.GetVersion",
			Err:  ErrInvalidPath,
		}
	}

	if version < 1 {
		return nil, nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Manager.GetVersion",
			Err:  ErrVersionMustBePositive,
		}
	}

	if ctx.Err() != nil {
		return nil, nil, &errors.QBError{
			Code: errors.ErrTimeout,
			Op:   "Manager.GetVersion",
			Err:  ctx.Err(),
		}
	}

	data, metadata, err := m.backend.GetVersion(ctx, path, version)
	if err != nil {
		if stderrors.Is(err, ErrSecretNotFound) || stderrors.Is(err, ErrVersionNotFound) {
			return nil, nil, &errors.QBError{
				Code: errors.ErrNotFound,
				Op:   "Manager.GetVersion",
				Err:  err,
			}
		}
		return nil, nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "Manager.GetVersion",
			Err:  err,
		}
	}

	return data, metadata, nil
}

// List returns all secret paths matching the prefix.
func (m *Manager) List(ctx context.Context, prefix string) ([]string, error) {
	if ctx.Err() != nil {
		return nil, &errors.QBError{
			Code: errors.ErrTimeout,
			Op:   "Manager.List",
			Err:  ctx.Err(),
		}
	}

	paths, err := m.backend.List(ctx, prefix)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "Manager.List",
			Err:  err,
		}
	}

	return paths, nil
}

// Delete removes a secret (soft delete, keeps versions).
func (m *Manager) Delete(ctx context.Context, path string) error {
	if path == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Manager.Delete",
			Err:  ErrInvalidPath,
		}
	}

	if ctx.Err() != nil {
		return &errors.QBError{
			Code: errors.ErrTimeout,
			Op:   "Manager.Delete",
			Err:  ctx.Err(),
		}
	}

	if err := m.backend.Delete(ctx, path); err != nil {
		if stderrors.Is(err, ErrSecretNotFound) {
			return &errors.QBError{
				Code: errors.ErrNotFound,
				Op:   "Manager.Delete",
				Err:  err,
			}
		}
		return &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "Manager.Delete",
			Err:  err,
		}
	}

	if m.logger != nil {
		m.logger.WithField("path", path).Info("Secret deleted")
	}

	return nil
}

// Destroy permanently removes all versions of a secret.
func (m *Manager) Destroy(ctx context.Context, path string) error {
	if path == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Manager.Destroy",
			Err:  ErrInvalidPath,
		}
	}

	if ctx.Err() != nil {
		return &errors.QBError{
			Code: errors.ErrTimeout,
			Op:   "Manager.Destroy",
			Err:  ctx.Err(),
		}
	}

	if err := m.backend.Destroy(ctx, path); err != nil {
		if stderrors.Is(err, ErrSecretNotFound) {
			return &errors.QBError{
				Code: errors.ErrNotFound,
				Op:   "Manager.Destroy",
				Err:  err,
			}
		}
		return &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "Manager.Destroy",
			Err:  err,
		}
	}

	if m.logger != nil {
		m.logger.WithField("path", path).Info("Secret destroyed")
	}

	return nil
}

// ListVersions returns metadata for all versions of a secret.
func (m *Manager) ListVersions(ctx context.Context, path string) ([]*interfaces.SecretVersionMetadata, error) {
	if path == "" {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Manager.ListVersions",
			Err:  ErrInvalidPath,
		}
	}

	if ctx.Err() != nil {
		return nil, &errors.QBError{
			Code: errors.ErrTimeout,
			Op:   "Manager.ListVersions",
			Err:  ctx.Err(),
		}
	}

	versions, err := m.backend.ListVersions(ctx, path)
	if err != nil {
		if stderrors.Is(err, ErrSecretNotFound) {
			return nil, &errors.QBError{
				Code: errors.ErrNotFound,
				Op:   "Manager.ListVersions",
				Err:  err,
			}
		}
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "Manager.ListVersions",
			Err:  err,
		}
	}

	// Convert from backend VersionMetadata to interfaces.SecretVersionMetadata
	result := make([]*interfaces.SecretVersionMetadata, len(versions))
	for i, v := range versions {
		result[i] = &interfaces.SecretVersionMetadata{
			Version:   v.Version,
			CreatedAt: v.CreatedAt,
			Deleted:   v.Deleted,
			DeletedAt: v.DeletedAt,
		}
	}

	return result, nil
}

// Rollback restores a previous version as the current version.
func (m *Manager) Rollback(ctx context.Context, path string, version int) error {
	if path == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Manager.Rollback",
			Err:  ErrInvalidPath,
		}
	}

	if version < 1 {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Manager.Rollback",
			Err:  ErrVersionMustBePositive,
		}
	}

	if ctx.Err() != nil {
		return &errors.QBError{
			Code: errors.ErrTimeout,
			Op:   "Manager.Rollback",
			Err:  ctx.Err(),
		}
	}

	if err := m.backend.Rollback(ctx, path, version); err != nil {
		if stderrors.Is(err, ErrSecretNotFound) || stderrors.Is(err, ErrVersionNotFound) {
			return &errors.QBError{
				Code: errors.ErrNotFound,
				Op:   "Manager.Rollback",
				Err:  err,
			}
		}
		return &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "Manager.Rollback",
			Err:  err,
		}
	}

	if m.logger != nil {
		m.logger.WithFields(map[string]interface{}{
			"path":    path,
			"version": version,
		}).Info("Secret rolled back")
	}

	return nil
}

// GetBackendType returns the backend type.
func (m *Manager) GetBackendType() string {
	return m.backend.Type()
}

// Close releases any resources held by the secrets manager.
func (m *Manager) Close() error {
	if m.logger != nil {
		m.logger.WithField("backend", m.backend.Type()).Info("Closing Secrets Manager")
	}

	if err := m.backend.Close(); err != nil {
		return &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "Manager.Close",
			Err:  &SecretsSentinelCauseError{Sentinel: ErrCloseBackendFailed, Cause: err},
		}
	}

	return nil
}

// enrichOptionsFromContext auto-populates StoreOptions fields from context.
// If TenantID is not explicitly set in opts, it will be extracted from the context.
// This enables transparent multi-tenant encryption without requiring callers
// to manually specify the tenant ID.
func (m *Manager) enrichOptionsFromContext(ctx context.Context, opts *interfaces.StoreOptions) *interfaces.StoreOptions {
	// If opts is nil, create a new one
	if opts == nil {
		opts = &interfaces.StoreOptions{}
	}

	// Auto-populate TenantID from context if not explicitly set
	if opts.TenantID == "" {
		tenantID := iam.TenantFromContext(ctx)
		if tenantID != "" {
			opts.TenantID = tenantID
			if m.logger != nil {
				m.logger.WithField("tenant_id", tenantID).Debug("Auto-populated TenantID from context")
			}
		}
	}

	return opts
}

// TenantIDFromContext extracts the tenant ID from the context.
// This is a convenience method for callers who need the tenant ID.
func (m *Manager) TenantIDFromContext(ctx context.Context) string {
	return iam.TenantFromContext(ctx)
}

// EnsureSecret returns the bytes stored at path, generating and
// persisting a fresh value on the FIRST call if no secret exists there.
// Subsequent calls return the same stable bytes. Concurrent callers
// racing on the same path are serialized via the distributed lock
// service so only one call actually generates — the others block until
// the first caller commits, then read the already-stored value.
//
// The generated return value reports whether THIS call produced the
// fresh value (true) or read an already-stored value (false). Exactly
// one caller in a racing set will see generated=true; the others see
// false. The flag is computed INSIDE the lock-held critical section so
// the reading is atomic with respect to the Get/Generate/Store
// transition — a caller who saw generated=true definitively wrote
// the bytes that every subsequent caller will read.
//
// This is the Vault-style generate-on-first-access primitive that the
// old AutoGenResolver pretended to provide but never did: the previous
// code generated fresh random bytes per call and returned different
// values every boot, leaving no way to bind a persistent secret to a
// configuration field. EnsureSecret is the real fix.
//
// Spec enforcement:
//
//   - First call ever at path: spec.Validate() → spec.Generate() → Store
//     with spec captured in metadata. Returns the generated bytes.
//
//   - Subsequent call with matching spec: Get → return stored bytes.
//     The spec comparison uses SecretSpec.Equal (length + encoding).
//
//   - Subsequent call with DIFFERENT spec: returns ErrSecretSpecMismatch
//     with no rotation. Caller must reconcile explicitly — this
//     prevents accidental secret rotation driven by a careless config
//     edit. To intentionally rotate, the operator Store()s a new value
//     (bumping the version) or Delete()s the secret first.
//
//   - Path previously populated by operator Store (no auto_generated
//     metadata marker): returns the stored bytes under
//     EnsureSecretDataKey if present, otherwise returns
//     ErrSecretNotFound-wrapped error indicating EnsureSecret cannot
//     adopt an operator-written secret with an unknown shape. Callers
//     should not mix paths between Store() and EnsureSecret().
//
// Distributed locking:
//
// When ManagerOptions.Locker was non-nil at construction, EnsureSecret
// acquires lock.LockOptions{Key: "secrets/ensure/"+path, TTL: 10s}
// before the read-or-generate critical section and releases on return.
// The key is scoped so unrelated paths do not contend. The lock works
// identically on standalone Pebble and Raft clusters because the qrdb
// lock service is composed of primitives both backends implement.
//
// When ManagerOptions.Locker was nil, EnsureSecret falls back to a
// process-local sync.Mutex keyed by path. This is ONLY safe in embedded
// single-process deployments. Cluster deployments without a configured
// Locker will produce undefined behavior on concurrent first-ever
// calls for the same path.
//
// The returned []byte is the raw value as the caller should consume
// it (for base64/hex/numeric encodings these are printable ASCII; for
// raw encoding these are binary bytes).
func (m *Manager) EnsureSecret(ctx context.Context, path string, spec *SecretSpec) (data []byte, generated bool, err error) {
	if path == "" {
		return nil, false, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Manager.EnsureSecret",
			Err:  ErrInvalidPath,
		}
	}
	if specErr := spec.Validate(); specErr != nil {
		return nil, false, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Manager.EnsureSecret",
			Err:  specErr,
		}
	}
	if ctx.Err() != nil {
		return nil, false, &errors.QBError{
			Code: errors.ErrTimeout,
			Op:   "Manager.EnsureSecret",
			Err:  ctx.Err(),
		}
	}

	// Acquire the lock. The distributed locker is preferred; the
	// local mutex is a last-resort fallback for embedded callers.
	release, lockErr := m.acquireEnsureLock(ctx, path)
	if lockErr != nil {
		return nil, false, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "Manager.EnsureSecret",
			Err:  lockErr,
		}
	}
	defer release()

	// Critical section (locked): try to read an existing value. A
	// stored secret with a matching spec is returned immediately
	// with generated=false. A stored secret with a DIFFERENT spec
	// is a hard error. A not-found error triggers the generate
	// path which returns generated=true.
	storedData, metadata, getErr := m.backend.Get(ctx, path)
	if getErr == nil {
		storedSpec, metaErr := specFromMetadata(metadata)
		if metaErr != nil {
			return nil, false, &errors.QBError{
				Code: errors.ErrInternal,
				Op:   "Manager.EnsureSecret",
				Err:  metaErr,
			}
		}
		if storedSpec == nil {
			return nil, false, &errors.QBError{
				Code: errors.ErrInvalidInput,
				Op:   "Manager.EnsureSecret",
				Err: stderrors.New(
					"secrets: path is populated by a non-auto-generated secret; " +
						"EnsureSecret will not adopt operator-managed values — " +
						"use Get() directly or choose a different path"),
			}
		}
		if !storedSpec.Equal(spec) {
			return nil, false, &errors.QBError{
				Code: errors.ErrInvalidInput,
				Op:   "Manager.EnsureSecret",
				Err: stderrors.Join(
					ErrSecretSpecMismatch,
					stderrors.New("stored spec length/encoding does not match caller's requested spec"),
				),
			}
		}
		value, ok := storedData[EnsureSecretDataKey]
		if !ok {
			return nil, false, &errors.QBError{
				Code: errors.ErrInternal,
				Op:   "Manager.EnsureSecret",
				Err: stderrors.New(
					"secrets: auto-generated secret is missing the canonical value key; " +
						"storage is corrupt for this path"),
			}
		}
		if m.logger != nil {
			m.logger.WithField("path", path).Debug("Secret ensured (cache hit)")
		}
		return value, false, nil
	}

	// Any error other than NotFound / Deleted is fatal.
	if !stderrors.Is(getErr, ErrSecretNotFound) && !stderrors.Is(getErr, ErrSecretDeleted) {
		return nil, false, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "Manager.EnsureSecret",
			Err:  getErr,
		}
	}

	// Generate a fresh value per spec and persist.
	freshBytes, genErr := spec.Generate()
	if genErr != nil {
		return nil, false, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "Manager.EnsureSecret",
			Err:  genErr,
		}
	}

	newData := map[string][]byte{EnsureSecretDataKey: freshBytes}
	newMeta := specToMetadata(spec, nil)

	if storeErr := m.backend.Store(ctx, path, newData, newMeta); storeErr != nil {
		// Best-effort: zero the generated bytes on store failure so
		// we do not leak entropy up the stack as a misleading
		// success.
		for i := range freshBytes {
			freshBytes[i] = 0
		}
		return nil, false, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "Manager.EnsureSecret",
			Err:  storeErr,
		}
	}

	if m.logger != nil {
		m.logger.WithField("path", path).
			WithField("encoding", spec.Encoding).
			WithField("length", spec.Length).
			Info("Secret ensured (generated + stored)")
	}

	return freshBytes, true, nil
}

// acquireEnsureLock obtains the per-path serialization lock used by
// EnsureSecret. It prefers the configured distributed Locker and falls
// back to a process-local sync.Mutex only when no Locker was provided.
// The returned release function is always safe to call exactly once;
// it never returns an error because there is nothing the caller can do
// to recover a lost lock at that point.
func (m *Manager) acquireEnsureLock(ctx context.Context, path string) (func(), error) {
	if m.locker != nil {
		lockKey := "secrets/ensure/" + path
		acquired, err := m.locker.Lock(ctx, qrdbsdk.LockOptions{
			Key: lockKey,
			TTL: ensureLockTTL,
		})
		if err != nil {
			return nil, err
		}
		return func() {
			// Release using the acquired fencing token so any
			// concurrent TryLock caller that raced on the same
			// key sees the correct transition.
			if unlockErr := m.locker.Unlock(context.Background(), lockKey, acquired.FencingToken); unlockErr != nil {
				if m.logger != nil {
					m.logger.WithField("path", path).
						WithField("error", unlockErr.Error()).
						Warn("EnsureSecret: failed to release distributed lock")
				}
			}
		}, nil
	}

	// No distributed locker: fall back to a process-local mutex
	// keyed by path. This path is OK for single-process embedded
	// deployments (unit tests, embedded BaaS). Multi-process or
	// cluster deployments must pass a Locker via ManagerOptions.
	mu := m.processLocalMutexFor(path)
	mu.Lock()
	return func() { mu.Unlock() }, nil
}

// processLocalMutexFor returns the sync.Mutex associated with a given
// path in the process-local ensureMu map, creating one on first use.
// Each unique path gets its own mutex so EnsureSecret calls for
// unrelated paths do not block each other.
func (m *Manager) processLocalMutexFor(path string) *sync.Mutex {
	if v, ok := m.ensureMu.Load(path); ok {
		mu, ok := v.(*sync.Mutex)
		if !ok {
			panic("orchestration: ensureMu contains non-*sync.Mutex value")
		}
		return mu
	}
	fresh := &sync.Mutex{}
	actual, _ := m.ensureMu.LoadOrStore(path, fresh)
	mu, ok := actual.(*sync.Mutex)
	if !ok {
		panic("secrets: ensureMu LoadOrStore returned non-*sync.Mutex value")
	}
	return mu
}
