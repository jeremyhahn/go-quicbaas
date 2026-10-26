//go:build !integration

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gossh "golang.org/x/crypto/ssh"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

func testHostPublicKey(t *testing.T) gossh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	key, err := gossh.NewPublicKey(pub)
	require.NoError(t, err)
	return key
}

// TestRemoteHostKeyStore_RoundTripsPinsThroughTheKnownHostsClient proves the
// git provisioner's pin store can be the key service's KnownHosts() client:
// a first-contact pin added through it is read back, listed and removed with
// the same public key, so a remote xkmsd keeps the pins its keys sign for.
func TestRemoteHostKeyStore_RoundTripsPinsThroughTheKnownHostsClient(t *testing.T) {
	ctx := qrdbsdk.ContextWithTenant(context.Background(), "tenant-a")
	store := newRemoteHostKeyStore(newTenantSSHClient(t).KnownHosts())
	key := testHostPublicKey(t)
	pin := xkmssdk.SSHPinFromPublicKey("git.example.com", xkmssdk.SSHScopeTenant, "tenant-a", key, "first-contact")

	require.NoError(t, store.Add(ctx, pin))

	got, err := store.Get(ctx, xkmssdk.SSHScopeTenant, "tenant-a", "git.example.com")
	require.NoError(t, err)
	assert.Equal(t, pin.Fingerprint, got.Fingerprint)
	assert.Equal(t, key.Marshal(), got.PublicKey)
	assert.Equal(t, xkmssdk.SSHScopeTenant, got.Scope)

	pins, err := store.List(ctx, xkmssdk.SSHScopeTenant, "tenant-a")
	require.NoError(t, err)
	require.Len(t, pins, 1)
	assert.Equal(t, "git.example.com", pins[0].Host)

	require.NoError(t, store.Remove(ctx, xkmssdk.SSHScopeTenant, "tenant-a", "git.example.com"))
	_, err = store.Get(ctx, xkmssdk.SSHScopeTenant, "tenant-a", "git.example.com")
	var notFound *xkmssdk.SSHKnownHostsNotFoundError
	assert.ErrorAs(t, err, &notFound)
}

// TestRemoteHostKeyStore_UnpinnedHostIsNotFound proves an unpinned host
// reports the store's *NotFoundError — the signal accept-new keys
// first-contact pinning on — and a pin without a public key is refused.
func TestRemoteHostKeyStore_UnpinnedHostIsNotFound(t *testing.T) {
	ctx := qrdbsdk.ContextWithTenant(context.Background(), "tenant-a")
	store := newRemoteHostKeyStore(newTenantSSHClient(t).KnownHosts())

	_, err := store.Get(ctx, xkmssdk.SSHScopeTenant, "tenant-a", "unpinned.example.com")
	var notFound *xkmssdk.SSHKnownHostsNotFoundError
	require.ErrorAs(t, err, &notFound)
	assert.Equal(t, "unpinned.example.com", notFound.Host)

	err = store.Remove(ctx, xkmssdk.SSHScopeTenant, "tenant-a", "unpinned.example.com")
	assert.ErrorAs(t, err, &notFound)

	err = store.Add(ctx, &xkmssdk.SSHKnownHostsPin{
		Host: "nokey.example.com", Scope: xkmssdk.SSHScopeTenant, Owner: "tenant-a",
	})
	var pinErr *SSHRemotePinError
	assert.ErrorAs(t, err, &pinErr)
}

// TestGitHostKeyStore_RemoteKeyServiceServesThePins proves a node whose SSH
// keys live on a remote xkmsd takes its git host-key pins from that service
// rather than the local sealed keyspace, which a remote-key node may not have.
func TestGitHostKeyStore_RemoteKeyServiceServesThePins(t *testing.T) {
	s := &Server{config: &config.Config{XKMS: config.XKMSConfig{URL: "https://xkmsd.test:8443"}}}

	hostKeys, err := s.gitHostKeyStore(newTenantSSHClient(t))

	require.NoError(t, err)
	assert.IsType(t, &remoteHostKeyStore{}, hostKeys)
}
