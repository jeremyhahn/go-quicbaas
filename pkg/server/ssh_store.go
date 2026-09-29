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
	"strings"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"

	qbshards "github.com/jeremyhahn/go-quicbaas/pkg/shards"
	"github.com/jeremyhahn/go-quicbaas/pkg/sshstore"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// sshStoreKeyPrefix namespaces the SSH key store inside the platform shard's
// system keyspace, which tenant KV operations cannot reach.
const sshStoreKeyPrefix = "__sys:ssh/"

// sshStoreMigrationTimeout bounds the one-time move of a legacy plaintext SSH
// store into the sealed store at boot.
const sshStoreMigrationTimeout = 2 * time.Minute

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

// SSHStoreMigrationError reports that the legacy plaintext SSH store could not
// be moved into the sealed store. The boot stops rather than serve SSH with
// keys missing.
type SSHStoreMigrationError struct {
	LegacyDir string
	Err       error
}

// Error implements the error interface.
func (e *SSHStoreMigrationError) Error() string {
	return "server: migrate legacy ssh store " + e.LegacyDir + ": " + e.Err.Error()
}

// Unwrap returns the underlying failure.
func (e *SSHStoreMigrationError) Unwrap() error { return e.Err }

// buildSSHClient composes the client behind Security().SSH(), mirroring
// xkmsd's own SSH wiring:
//
//   - xkms.url set: the REST client for that remote xkmsd, which holds the
//     keys in its own sealed storage.
//   - otherwise: the embedded go-xkms client over the node's barrier-sealed
//     platform keyspace. Its backend provider mints and signs backend-backed
//     keys on the registered xkms crypto backends. A legacy plaintext store
//     under the data dir is migrated in and destroyed first.
//
// A node whose data plane is not barrier-sealed gets no client (nil, nil). Its
// SSH verbs fail closed with *service.SSHNotConfiguredError.
func (s *Server) buildSSHClient() (xkmsssh.Client, error) {
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

	provider, err := xkmsssh.NewBackendProvider(resolveSSHKeyProvider)
	if err != nil {
		return nil, &SSHStoreBuildError{Op: "backend provider", Err: err}
	}
	if err := s.migrateLegacySSHStore(sealed, provider); err != nil {
		return nil, err
	}
	client, err := xkmsssh.NewEmbedded(xkmsssh.EmbeddedOptions{Backend: sealed, BackendProvider: provider})
	if err != nil {
		return nil, &SSHStoreBuildError{Op: "embedded client", Err: err}
	}
	s.logger.Info("SSH key store served from barrier-sealed storage")
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
	backend, err := xkmssdk.NewStoragePrefixBackend(xkmssdk.NewQRDBStorageBackend(shardClient.KV()), sshStoreKeyPrefix)
	if err != nil {
		return nil, &SSHStoreBuildError{Op: "sealed storage", Err: err}
	}
	return backend, nil
}

// migrateLegacySSHStore moves a legacy plaintext store under the data dir into
// sealed and destroys it, logging what moved.
func (s *Server) migrateLegacySSHStore(sealed xkmssdk.StorageBackend, provider xkmsssh.BackendProvider) error {
	dataDir := s.config.Storage.DataDir
	if dataDir == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), sshStoreMigrationTimeout)
	defer cancel()

	legacyDir := sshstore.LegacyDir(dataDir)
	report, err := sshstore.MigrateLegacy(ctx, legacyDir, sealed, provider)
	if err != nil {
		return &SSHStoreMigrationError{LegacyDir: legacyDir, Err: err}
	}
	if !report.Found {
		return nil
	}
	s.logger.WithFields(map[string]interface{}{
		"legacy_dir":      report.LegacyDir,
		"keys":            report.Keys,
		"known_hosts":     report.KnownHosts,
		"authorized_keys": report.AuthorizedKeys,
		"other_records":   report.Other,
		"resumed_wipe":    report.ResumedWipe,
	}).Info("SSH key store migrated into barrier-sealed storage; legacy plaintext store destroyed")
	return nil
}

// reportUnservedSSHStore logs why the node has no SSH key store and names a
// legacy plaintext store that is left on disk, neither served nor migrated.
func (s *Server) reportUnservedSSHStore(cause error) {
	s.logger.WithError(cause).Warn("SSH key store unavailable: every SSH verb fails with ssh-not-configured")
	dataDir := s.config.Storage.DataDir
	if dataDir == "" {
		return
	}
	legacyDir := sshstore.LegacyDir(dataDir)
	present, err := sshstore.LegacyPresent(legacyDir)
	if err != nil {
		s.logger.WithError(err).Warn("SSH key store: cannot inspect the legacy plaintext store")
		return
	}
	if present {
		s.logger.WithField("legacy_dir", legacyDir).Warn(
			"SSH key store: a legacy plaintext SSH store is on disk and is neither served nor migrated; enable the barrier and restart to move it into sealed storage")
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
