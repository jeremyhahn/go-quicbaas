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

package service

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	stderrors "errors"
	"testing"

	gossh "golang.org/x/crypto/ssh"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"

	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/sshfake"
)

// =============================================================================
// Happy paths
// =============================================================================

func TestSSHService_KeyLifecycle(t *testing.T) {
	fake := sshfake.NewClient()
	svc := NewSSHService(fake)
	ctx := context.Background()

	key, err := svc.GenerateKey(ctx, xkmsssh.GenerateRequest{
		Scope: "user", Owner: "alice", Name: "laptop", Algorithm: "ed25519",
	})
	require.NoError(t, err)
	assert.Equal(t, "laptop", key.Name)

	got, err := svc.GetKey(ctx, xkmsssh.GetKeyRequest{Scope: "user", Owner: "alice", Name: "laptop"})
	require.NoError(t, err)
	assert.Equal(t, "laptop", got.Name)

	list, err := svc.ListKeys(ctx, xkmsssh.ListKeysRequest{Scope: "user", Owner: "alice"})
	require.NoError(t, err)
	require.Len(t, list, 1)

	pub, err := svc.ExportPublicKey(ctx, xkmsssh.ExportRequest{Scope: "user", Owner: "alice", Name: "laptop"})
	require.NoError(t, err)
	assert.Contains(t, pub.Line, "ssh-ed25519")

	require.NoError(t, svc.DeleteKey(ctx, xkmsssh.DeleteKeyRequest{Scope: "user", Owner: "alice", Name: "laptop"}))

	list, err = svc.ListKeys(ctx, xkmsssh.ListKeysRequest{})
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestSSHService_ImportKeyDecodesPassphrase(t *testing.T) {
	fake := sshfake.NewClient()
	svc := NewSSHService(fake)

	key, err := svc.ImportKey(context.Background(), SSHImportKeyRequest{
		Scope: "user", Owner: "alice", Name: "imported",
		PEM:           []byte("-----BEGIN OPENSSH PRIVATE KEY-----"),
		PassphraseB64: base64.StdEncoding.EncodeToString([]byte("hunter2")),
	})
	require.NoError(t, err)
	assert.Equal(t, "imported", key.Name)

	// The mlocked passphrase reached the xkms client intact.
	assert.Equal(t, "hunter2", fake.LastImportPassphrase)
}

func TestSSHService_ImportKeyWithoutPassphrasePassesNilProvider(t *testing.T) {
	fake := sshfake.NewClient()
	svc := NewSSHService(fake)

	_, err := svc.ImportKey(context.Background(), SSHImportKeyRequest{
		Scope: "user", Owner: "alice", Name: "plain",
		PEM: []byte("-----BEGIN OPENSSH PRIVATE KEY-----"),
	})
	require.NoError(t, err)
	assert.Empty(t, fake.LastImportPassphrase, "no provider should have been invoked")
}

func TestSSHService_SignDigest(t *testing.T) {
	fake := sshfake.NewClient()
	fake.SignerPublicKey = testSSHPublicKey(t)
	svc := NewSSHService(fake)

	sig, err := svc.SignDigest(context.Background(), SSHSignRequest{
		Scope: "user", Owner: "alice", Name: "laptop", Digest: []byte("digest"),
	})
	require.NoError(t, err)
	assert.Equal(t, "ssh-ed25519", sig.Format)
	assert.Equal(t, []byte("sig"), sig.Blob)
}

func TestSSHService_KnownHostLifecycle(t *testing.T) {
	fake := sshfake.NewClient()
	svc := NewSSHService(fake)
	ctx := context.Background()

	require.NoError(t, svc.AddKnownHost(ctx, xkmsssh.AddKnownHostRequest{
		Scope: "user", Owner: "alice", Host: "git.example.com", Fingerprint: "SHA256:abc",
	}))

	pins, err := svc.ListKnownHosts(ctx, xkmsssh.ListKnownHostsRequest{Scope: "user", Owner: "alice"})
	require.NoError(t, err)
	require.Len(t, pins, 1)
	assert.Equal(t, "git.example.com", pins[0].Host)

	require.NoError(t, svc.RemoveKnownHost(ctx, xkmsssh.RemoveKnownHostRequest{
		Scope: "user", Owner: "alice", Host: "git.example.com",
	}))
	pins, err = svc.ListKnownHosts(ctx, xkmsssh.ListKnownHostsRequest{})
	require.NoError(t, err)
	assert.Empty(t, pins)
}

func TestSSHService_AuthorizedKeyLifecycle(t *testing.T) {
	fake := sshfake.NewClient()
	svc := NewSSHService(fake)
	ctx := context.Background()

	require.NoError(t, svc.AddAuthorizedKey(ctx, xkmsssh.AddAuthorizedKeyRequest{
		Scope: "user", Owner: "alice", Name: "deploy", Line: "ssh-ed25519 AAAA deploy",
	}))

	entries, err := svc.ListAuthorizedKeys(ctx, xkmsssh.ListAuthorizedKeysRequest{Scope: "user", Owner: "alice"})
	require.NoError(t, err)
	require.Len(t, entries, 1)

	line, err := svc.EmitAuthorizedKey(ctx, xkmsssh.EmitRequest{
		PublicKey: "ssh-ed25519 AAAA", Comment: "alice@laptop",
	})
	require.NoError(t, err)
	assert.Equal(t, "ssh-ed25519 AAAA alice@laptop", line)

	require.NoError(t, svc.RemoveAuthorizedKey(ctx, SSHAuthorizedKeyRef{
		Scope: "user", Owner: "alice", Name: "deploy",
	}))
	entries, err = svc.ListAuthorizedKeys(ctx, xkmsssh.ListAuthorizedKeysRequest{})
	require.NoError(t, err)
	assert.Empty(t, entries)
}

// =============================================================================
// Error paths
// =============================================================================

func TestSSHService_NilClientReportsNotConfigured(t *testing.T) {
	svc := NewSSHService(nil)
	ctx := context.Background()

	var notConfigured *SSHNotConfiguredError

	_, err := svc.GenerateKey(ctx, xkmsssh.GenerateRequest{Name: "k"})
	require.ErrorAs(t, err, &notConfigured)

	_, err = svc.ImportKey(ctx, SSHImportKeyRequest{Name: "k"})
	require.ErrorAs(t, err, &notConfigured)

	_, err = svc.GetKey(ctx, xkmsssh.GetKeyRequest{Name: "k"})
	require.ErrorAs(t, err, &notConfigured)

	_, err = svc.ListKeys(ctx, xkmsssh.ListKeysRequest{})
	require.ErrorAs(t, err, &notConfigured)

	require.ErrorAs(t, svc.DeleteKey(ctx, xkmsssh.DeleteKeyRequest{Name: "k"}), &notConfigured)

	_, err = svc.ExportPublicKey(ctx, xkmsssh.ExportRequest{Name: "k"})
	require.ErrorAs(t, err, &notConfigured)

	_, err = svc.SignDigest(ctx, SSHSignRequest{Name: "k", Digest: []byte("d")})
	require.ErrorAs(t, err, &notConfigured)

	require.ErrorAs(t, svc.AddKnownHost(ctx, xkmsssh.AddKnownHostRequest{Host: "h"}), &notConfigured)

	_, err = svc.ListKnownHosts(ctx, xkmsssh.ListKnownHostsRequest{})
	require.ErrorAs(t, err, &notConfigured)

	require.ErrorAs(t, svc.RemoveKnownHost(ctx, xkmsssh.RemoveKnownHostRequest{Host: "h"}), &notConfigured)
	require.ErrorAs(t, svc.AddAuthorizedKey(ctx, xkmsssh.AddAuthorizedKeyRequest{Name: "n"}), &notConfigured)

	_, err = svc.ListAuthorizedKeys(ctx, xkmsssh.ListAuthorizedKeysRequest{})
	require.ErrorAs(t, err, &notConfigured)

	require.ErrorAs(t, svc.RemoveAuthorizedKey(ctx, SSHAuthorizedKeyRef{Name: "n"}), &notConfigured)

	_, err = svc.EmitAuthorizedKey(ctx, xkmsssh.EmitRequest{PublicKey: "k"})
	require.ErrorAs(t, err, &notConfigured)
}

func TestSSHService_SignDigestRejectsEmptyDigest(t *testing.T) {
	svc := NewSSHService(sshfake.NewClient())
	_, err := svc.SignDigest(context.Background(), SSHSignRequest{Name: "laptop"})
	var missing *SSHMissingDigestError
	require.ErrorAs(t, err, &missing)
}

func TestSSHService_ImportKeyRejectsBadBase64Passphrase(t *testing.T) {
	svc := NewSSHService(sshfake.NewClient())
	_, err := svc.ImportKey(context.Background(), SSHImportKeyRequest{
		Name: "k", PEM: []byte("pem"), PassphraseB64: "!!!not base64!!!",
	})
	var decodeErr *SSHPassphraseDecodeError
	require.ErrorAs(t, err, &decodeErr)
	assert.NotNil(t, stderrors.Unwrap(decodeErr))
}

func TestSSHService_PropagatesBackendError(t *testing.T) {
	backendErr := &xkmsssh.NotFoundError{Resource: "key", Message: "missing"}
	fake := sshfake.NewClient()
	fake.Err = backendErr
	svc := NewSSHService(fake)
	ctx := context.Background()

	_, err := svc.GetKey(ctx, xkmsssh.GetKeyRequest{Name: "missing"})
	require.ErrorIs(t, err, backendErr)

	_, err = svc.ListKeys(ctx, xkmsssh.ListKeysRequest{})
	require.ErrorIs(t, err, backendErr)

	require.ErrorIs(t, svc.AddKnownHost(ctx, xkmsssh.AddKnownHostRequest{Host: "h", Fingerprint: "f"}), backendErr)
	require.ErrorIs(t, svc.AddAuthorizedKey(ctx, xkmsssh.AddAuthorizedKeyRequest{Name: "n", Line: "l"}), backendErr)

	_, err = svc.SignDigest(ctx, SSHSignRequest{Name: "k", Digest: []byte("d")})
	require.ErrorIs(t, err, backendErr)
}

func TestSSHService_SignDigestSurfacesMissingSigner(t *testing.T) {
	// SignerPublicKey unset => the fake reports NotFound from Signer().
	svc := NewSSHService(sshfake.NewClient())
	_, err := svc.SignDigest(context.Background(), SSHSignRequest{Name: "absent", Digest: []byte("d")})
	var notFound *xkmsssh.NotFoundError
	require.ErrorAs(t, err, &notFound)
}

// testSSHPublicKey builds a throwaway ed25519 SSH public key.
func testSSHPublicKey(t *testing.T) gossh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	sshPub, err := gossh.NewPublicKey(pub)
	require.NoError(t, err)
	return sshPub
}
