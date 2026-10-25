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

package server

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"

	qbshards "github.com/jeremyhahn/go-quicbaas/pkg/shards"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// sshStoreKeyPrefix namespaces the SSH key store inside the platform shard's
// system keyspace, which tenant KV operations cannot reach.
const sshStoreKeyPrefix = "__sys:ssh/"

// sshTenantKeyPrefix namespaces a non-system tenant's SSH keys under
// "__sys:ssh-t/{tenant}/", as pkg/scheduler/placement does its records.
const sshTenantKeyPrefix = "__sys:ssh-t/"

// legacySSHStoreDirNames are the plaintext SSH key directories that builds
// before go-xkms v0.55 kept under the data dir: the store itself and the
// rename target of an interrupted wipe. Nothing reads them any more.
var legacySSHStoreDirNames = []string{"xkms-ssh", "xkms-ssh.wiping"}

// SSHStoreNotSealedError reports that the node has no SSH key store: its data
// plane is not barrier-sealed and no remote xkmsd is configured. Private keys
// are never kept on the filesystem, so there is no other place to hold them.
type SSHStoreNotSealedError struct{}

// Error implements the error interface.
func (e *SSHStoreNotSealedError) Error() string {
	return "server: no ssh key store: the node's data plane is not barrier-sealed (seal is disabled) and xkms.url names no remote xkmsd"
}

// SSHStoreBuildError wraps a failure composing the SSH key store client. Op
// names the stage that failed.
type SSHStoreBuildError struct {
	Op  string
	Err error
}

// Error implements the error interface.
func (e *SSHStoreBuildError) Error() string {
	return "server: ssh key store: " + e.Op + ": " + e.Err.Error()
}

// Unwrap returns the underlying failure.
func (e *SSHStoreBuildError) Unwrap() error { return e.Err }

// buildSSHClient composes the client behind Security().SSH(), mirroring
// xkmsd's own SSH wiring:
//
//   - xkms.url set: the REST client for that remote xkmsd, which holds the
//     keys in its own sealed storage.
//   - otherwise: the embedded go-xkms client over the node's barrier-sealed
//     platform keyspace. Its backend provider mints and signs backend-backed
//     keys on the registered xkms crypto backends. Records are go-xkms DAO
//     entities; the pre-v1 plaintext and key-value layouts are not read.
//
// A node whose data plane is not barrier-sealed gets no client (nil, nil). Its
// SSH verbs fail closed with *service.SSHNotConfiguredError.
func (s *Server) buildSSHClient() (xkmsssh.Client, error) {
	s.warnLegacySSHStore()

	if url := strings.TrimSpace(s.config.XKMS.URL); url != "" {
		client, err := xkmsssh.NewREST(xkmsssh.RESTOptions{
			BaseURL:   url,
			AuthToken: strings.TrimSpace(s.config.XKMS.Token),
		})
		if err != nil {
			return nil, &SSHStoreBuildError{Op: "remote xkmsd client", Err: err}
		}
		return client, nil
	}

	sealed, err := s.sshSealedBackend()
	var notSealed *SSHStoreNotSealedError
	if errors.As(err, &notSealed) {
		s.reportUnservedSSHStore(err)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	client, err := newEmbeddedSSHClient(sealed)
	if err != nil {
		return nil, err
	}
	s.logger.Info("SSH key store served from barrier-sealed storage")
	return client, nil
}

// newEmbeddedSSHClient serves the go-xkms SSH key store from sealed.
func newEmbeddedSSHClient(sealed xkmssdk.StorageBackend) (xkmsssh.Client, error) {
	provider, err := xkmsssh.NewBackendProvider(resolveSSHKeyProvider)
	if err != nil {
		return nil, &SSHStoreBuildError{Op: "backend provider", Err: err}
	}
	daos, err := sshDAOFactory(sealed)
	if err != nil {
		return nil, err
	}
	stores, err := xkmsssh.NewStores(daos)
	if err != nil {
		return nil, &SSHStoreBuildError{Op: "stores", Err: err}
	}
	client, err := xkmsssh.NewEmbedded(xkmsssh.EmbeddedOptions{Stores: stores, BackendProvider: provider})
	if err != nil {
		return nil, &SSHStoreBuildError{Op: "embedded client", Err: err}
	}
	return client, nil
}

// sshSealedBackend returns the SSH store's storage: the platform shard's
// replicated, barrier-encrypted keyspace under sshStoreKeyPrefix. The node
// encrypts its data plane at rest with the barrier exactly when it has one,
// and it has one exactly when its BarrierRegistry is set.
func (s *Server) sshSealedBackend() (xkmssdk.StorageBackend, error) {
	node := s.getQRDBNode()
	if node == nil || node.BarrierRegistry() == nil {
		return nil, &SSHStoreNotSealedError{}
	}
	if s.qrdbClient == nil {
		return nil, &SSHStoreBuildError{Op: "platform shard client", Err: &QRDBClientNotInitializedError{}}
	}
	shardClient, err := s.qrdbClient.ForShard(qbshards.PlatformShardID)
	if err != nil {
		return nil, &SSHStoreBuildError{Op: "platform shard client", Err: err}
	}
	return s.sshStoreBackend(shardClient.KV())
}

// sshStoreBackend layers the SSH key store over kv, run as the node's
// service principal and confined per call to the caller's tenant partition.
func (s *Server) sshStoreBackend(kv transport.KVClient) (xkmssdk.StorageBackend, error) {
	backend := &sshTenantBackend{inner: xkmssdk.NewQRDBStorageBackend(s.serviceKV(kv))}
	if _, err := backend.partition(context.Background()); err != nil {
		return nil, &SSHStoreBuildError{Op: "sealed storage", Err: err}
	}
	return backend, nil
}

// sshPartition is the view of the SSH store through one tenant's prefix:
// the storage backend plus the guarded writes go-xkms's DAO adapter uses.
type sshPartition interface {
	xkmssdk.StorageBackend
	CompareAndSwap(ctx context.Context, key string, expected, newValue []byte) (bool, []byte, error)
	CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error)
	GuardedWritesAvailable() bool
}

// sshTenantBackend runs every SSH store call as the system tenant, inside
// the caller's tenant partition. The store's backend is the system service
// principal, and qrdb's self-scope check refuses a system identity acting
// for another tenant, so the backend call runs as the system tenant. The
// caller's tenant is not lost: it picks the key prefix, so one tenant never
// lists, reads or overwrites another's keys.
type sshTenantBackend struct {
	inner xkmssdk.StorageBackend
}

// sshPartitionPrefix returns the caller's partition: sshStoreKeyPrefix for
// the platform (no tenant or the system tenant), else
// sshTenantKeyPrefix{tenant}/.
func sshPartitionPrefix(ctx context.Context) string {
	tenantID, _ := tenantctx.Lookup(ctx)
	if qrdbsdk.IsSystemTenant(tenantID) {
		return sshStoreKeyPrefix
	}
	return sshTenantKeyPrefix + tenantID + "/"
}

// partition returns the caller's partition of the store.
func (b *sshTenantBackend) partition(ctx context.Context) (sshPartition, error) {
	return xkmssdk.NewStoragePrefixBackend(b.inner, sshPartitionPrefix(ctx))
}

// bind returns the caller's partition and the context its backend call
// runs under: the system tenant.
func (b *sshTenantBackend) bind(ctx context.Context) (sshPartition, context.Context, error) {
	p, err := b.partition(ctx)
	if err != nil {
		return nil, nil, err
	}
	return p, tenantctx.WithSystem(ctx), nil
}

func (b *sshTenantBackend) Get(ctx context.Context, key string) ([]byte, error) {
	p, ctx, err := b.bind(ctx)
	if err != nil {
		return nil, err
	}
	return p.Get(ctx, key)
}

func (b *sshTenantBackend) Put(ctx context.Context, key string, value []byte) error {
	p, ctx, err := b.bind(ctx)
	if err != nil {
		return err
	}
	return p.Put(ctx, key, value)
}

func (b *sshTenantBackend) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	p, ctx, err := b.bind(ctx)
	if err != nil {
		return false, err
	}
	return p.PutIfAbsent(ctx, key, value)
}

func (b *sshTenantBackend) Delete(ctx context.Context, key string) error {
	p, ctx, err := b.bind(ctx)
	if err != nil {
		return err
	}
	return p.Delete(ctx, key)
}

func (b *sshTenantBackend) List(ctx context.Context, prefix string) ([]string, error) {
	p, ctx, err := b.bind(ctx)
	if err != nil {
		return nil, err
	}
	return p.List(ctx, prefix)
}

func (b *sshTenantBackend) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	p, ctx, err := b.bind(ctx)
	if err != nil {
		return err
	}
	return p.Scan(ctx, prefix, fn)
}

func (b *sshTenantBackend) Exists(ctx context.Context, key string) (bool, error) {
	p, ctx, err := b.bind(ctx)
	if err != nil {
		return false, err
	}
	return p.Exists(ctx, key)
}

func (b *sshTenantBackend) CompareAndSwap(ctx context.Context, key string, expected, newValue []byte) (bool, []byte, error) {
	p, ctx, err := b.bind(ctx)
	if err != nil {
		return false, nil, err
	}
	return p.CompareAndSwap(ctx, key, expected, newValue)
}

func (b *sshTenantBackend) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	p, ctx, err := b.bind(ctx)
	if err != nil {
		return false, err
	}
	return p.CompareAndDelete(ctx, key, expected)
}

// GuardedWritesAvailable reports the inner backend's guarded-write support,
// which every partition shares.
func (b *sshTenantBackend) GuardedWritesAvailable() bool {
	p, err := b.partition(context.Background())
	return err == nil && p.GuardedWritesAvailable()
}

func (b *sshTenantBackend) Close() error {
	return b.inner.Close()
}

// sshDAOFactory builds the go-xkms DAO factory the SSH stores persist through.
func sshDAOFactory(sealed xkmssdk.StorageBackend) (*qrdbsdk.DAOFactory, error) {
	daos, err := xkmssdk.NewBackendDAOFactory(sealed)
	if err != nil {
		return nil, &SSHStoreBuildError{Op: "dao factory", Err: err}
	}
	return daos, nil
}

// sshHostKeyStore returns the host-key pin store over the barrier-sealed SSH
// keyspace: the same records the embedded SSH client's KnownHosts() serves.
// It returns *SSHStoreNotSealedError when the node has no barrier, so pins
// are never kept on the filesystem.
func (s *Server) sshHostKeyStore() (xkmsssh.HostKeyStore, error) {
	sealed, err := s.sshSealedBackend()
	if err != nil {
		return nil, err
	}
	daos, err := sshDAOFactory(sealed)
	if err != nil {
		return nil, err
	}
	hostKeys, err := xkmsssh.NewHostKeyStore(daos)
	if err != nil {
		return nil, &SSHStoreBuildError{Op: "host-key store", Err: err}
	}
	return hostKeys, nil
}

// reportUnservedSSHStore logs why the node has no SSH key store.
func (s *Server) reportUnservedSSHStore(cause error) {
	s.logger.WithError(cause).Warn("SSH key store unavailable: every SSH verb fails with ssh-not-configured")
}

// warnLegacySSHStore logs a warning for each legacy plaintext SSH key
// directory left under the data dir. It reads nothing from the directory,
// deletes nothing and never stops the boot: removing it is the operator's
// step after re-enrolment (docs/ssh/architecture.md).
func (s *Server) warnLegacySSHStore() {
	dataDir := s.config.Storage.DataDir
	if dataDir == "" {
		return
	}
	for _, name := range legacySSHStoreDirNames {
		legacyDir := filepath.Join(dataDir, name)
		info, err := os.Stat(legacyDir)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			s.logger.WithField("legacy_dir", legacyDir).WithError(err).Warn(
				"SSH key store: cannot inspect the legacy plaintext SSH key directory path")
			continue
		}
		if !info.IsDir() {
			continue
		}
		s.logger.WithField("legacy_dir", legacyDir).Warn(
			"SSH key store: a legacy SSH key directory holds plaintext private keys that are no longer read; " +
				"re-enrol the keys and remove the directory (see docs/ssh/architecture.md)")
	}
}

// resolveSSHKeyProvider returns the crypto KeyProvider of a registered xkms
// backend, so backend-backed SSH keys are minted and signed on that backend.
func resolveSSHKeyProvider(_ context.Context, backendID string) (xkmssdk.KeyProvider, error) {
	backend, err := dbxkms.GetBackend(backendID)
	if err != nil {
		return nil, err
	}
	return backend.KeyProvider(), nil
}
