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
	"os"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/sshstore"
)

var legacyDeployKey = xkmsssh.GetKeyRequest{Scope: "user", Owner: "alice", Name: "fleet-deploy"}

func newSSHStoreTestServer(t *testing.T, dataDir string, xkms config.XKMSConfig) *Server {
	t.Helper()
	return &Server{
		config: &config.Config{Storage: config.StorageConfig{DataDir: dataDir}, XKMS: xkms},
		logger: buildTestLogger(t),
	}
}

// seedLegacySSHStore writes one key into a legacy plaintext store under
// dataDir and returns its public key line.
func seedLegacySSHStore(t *testing.T, dataDir string) string {
	t.Helper()
	client, err := xkmsssh.NewEmbeddedFile(xkmsssh.EmbeddedFileOptions{RootDir: sshstore.LegacyDir(dataDir)})
	require.NoError(t, err)
	key, err := client.Keys().Generate(context.Background(), xkmsssh.GenerateRequest{
		Scope: legacyDeployKey.Scope, Owner: legacyDeployKey.Owner, Name: legacyDeployKey.Name, Algorithm: "ed25519",
	})
	require.NoError(t, err)
	require.NoError(t, client.Close())
	return key.PublicKey
}

func TestBuildSSHClient_RemoteXKMSD(t *testing.T) {
	s := newSSHStoreTestServer(t, t.TempDir(), config.XKMSConfig{URL: " https://xkmsd.example.internal:8443 ", Token: "t"})

	client, err := s.buildSSHClient()
	require.NoError(t, err)
	require.NotNil(t, client)
	require.NoError(t, client.Close())
}

// TestBuildSSHClient_UnsealedNodeFailsClosed proves a node with no barrier
// gets no SSH store, and that a legacy plaintext store on such a node is left
// exactly where it was: neither served nor migrated into unsealed storage.
func TestBuildSSHClient_UnsealedNodeFailsClosed(t *testing.T) {
	dataDir := t.TempDir()
	seedLegacySSHStore(t, dataDir)
	s := newSSHStoreTestServer(t, dataDir, config.XKMSConfig{})

	client, err := s.buildSSHClient()
	require.NoError(t, err)
	assert.Nil(t, client, "an unsealed node must have no SSH key store")

	legacy, err := xkmsssh.NewEmbeddedFile(xkmsssh.EmbeddedFileOptions{RootDir: sshstore.LegacyDir(dataDir)})
	require.NoError(t, err)
	_, err = legacy.Keys().Get(context.Background(), legacyDeployKey)
	require.NoError(t, err, "the legacy store must not be touched on an unsealed node")
	require.NoError(t, legacy.Close())
}

func TestSSHSealedBackend_NoBarrier(t *testing.T) {
	s := newSSHStoreTestServer(t, "", config.XKMSConfig{})

	backend, err := s.sshSealedBackend()
	assert.Nil(t, backend)
	var notSealed *SSHStoreNotSealedError
	require.ErrorAs(t, err, &notSealed)
}

func TestMigrateLegacySSHStore_MovesIntoSealed(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	pub := seedLegacySSHStore(t, dataDir)
	s := newSSHStoreTestServer(t, dataDir, config.XKMSConfig{})
	sealed, err := xkmssdk.NewMemoryBackend()
	require.NoError(t, err)

	require.NoError(t, s.migrateLegacySSHStore(sealed, nil))

	assert.NoDirExists(t, sshstore.LegacyDir(dataDir))
	client, err := xkmsssh.NewEmbedded(xkmsssh.EmbeddedOptions{Backend: sealed})
	require.NoError(t, err)
	got, err := client.Keys().Get(ctx, legacyDeployKey)
	require.NoError(t, err)
	assert.Equal(t, pub, got.PublicKey)
}

func TestMigrateLegacySSHStore_NoLegacyStore(t *testing.T) {
	tests := []struct {
		name    string
		dataDir string
	}{
		{name: "no data dir", dataDir: ""},
		{name: "empty data dir", dataDir: t.TempDir()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newSSHStoreTestServer(t, tt.dataDir, config.XKMSConfig{})
			sealed, err := xkmssdk.NewMemoryBackend()
			require.NoError(t, err)
			require.NoError(t, s.migrateLegacySSHStore(sealed, nil))
		})
	}
}

func TestMigrateLegacySSHStore_FailureStopsTheBoot(t *testing.T) {
	dataDir := t.TempDir()
	legacyDir := sshstore.LegacyDir(dataDir)
	require.NoError(t, os.WriteFile(legacyDir, []byte("not a store"), 0o600))
	s := newSSHStoreTestServer(t, dataDir, config.XKMSConfig{})
	sealed, err := xkmssdk.NewMemoryBackend()
	require.NoError(t, err)

	err = s.migrateLegacySSHStore(sealed, nil)
	var migrationErr *SSHStoreMigrationError
	require.ErrorAs(t, err, &migrationErr)
	assert.Equal(t, legacyDir, migrationErr.LegacyDir)
	var unexpected *sshstore.UnexpectedEntryError
	require.ErrorAs(t, err, &unexpected)
}

func TestResolveSSHKeyProvider_UnknownBackend(t *testing.T) {
	provider, err := resolveSSHKeyProvider(context.Background(), "no-such-backend")
	assert.Nil(t, provider)
	require.Error(t, err)
}

func TestSSHStoreErrorMessages(t *testing.T) {
	cause := errors.New("boom")
	tests := []struct {
		name  string
		err   error
		want  string
		cause error
	}{
		{name: "not sealed", err: &SSHStoreNotSealedError{}, want: "not barrier-sealed"},
		{name: "build", err: &SSHStoreBuildError{Op: "sealed storage", Err: cause}, want: "sealed storage: boom", cause: cause},
		{name: "migration", err: &SSHStoreMigrationError{LegacyDir: "/d/xkms-ssh", Err: cause}, want: "/d/xkms-ssh: boom", cause: cause},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Contains(t, tt.err.Error(), tt.want)
			if tt.cause != nil {
				assert.ErrorIs(t, tt.err, tt.cause)
			}
		})
	}
}
