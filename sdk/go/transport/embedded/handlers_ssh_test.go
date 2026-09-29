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

package embedded

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"

	gossh "golang.org/x/crypto/ssh"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/sshfake"
	sshclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/ssh"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/test/sshfixture"
)

// sshMockService overrides SSHClient on the package's base mock so the SSH
// handlers see an in-memory store.
type sshMockService struct {
	*mockQuicBaaS
	ssh xkmsssh.Client
}

func (m *sshMockService) SSHClient() xkmsssh.Client { return m.ssh }

// newSSHTestService returns a composed service whose SSH client is the
// supplied fake, plus the fake itself for assertions.
func newSSHTestService(fake *sshfake.Client) service.QuicBaaS {
	return &sshMockService{mockQuicBaaS: &mockQuicBaaS{}, ssh: fake}
}

// =============================================================================
// Happy paths
// =============================================================================

func TestEmbeddedSSHHandlers_KeyLifecycle(t *testing.T) {
	fake := sshfake.NewClient()
	svc := newSSHTestService(fake)
	ctx := context.Background()

	resp, err := handleSSHGenerateKey(ctx, svc, &sshclient.GenerateRequest{
		Scope: "user", Owner: "alice", Name: "laptop", Algorithm: "ed25519",
	})
	require.NoError(t, err)
	key, ok := resp.(*sshclient.Key)
	require.True(t, ok, "expected *ssh.Key, got %T", resp)
	assert.Equal(t, "laptop", key.Name)

	resp, err = handleSSHGetKey(ctx, svc, &sshclient.GetKeyRequest{
		Scope: "user", Owner: "alice", Name: "laptop",
	})
	require.NoError(t, err)
	assert.Equal(t, "laptop", resp.(*sshclient.Key).Name)

	resp, err = handleSSHListKeys(ctx, svc, &sshclient.ListKeysRequest{Scope: "user", Owner: "alice"})
	require.NoError(t, err)
	list, ok := resp.(*sshclient.ListKeysResponse)
	require.True(t, ok)
	require.Len(t, list.Items, 1)

	resp, err = handleSSHExportPublicKey(ctx, svc, &sshclient.ExportRequest{
		Scope: "user", Owner: "alice", Name: "laptop",
	})
	require.NoError(t, err)
	assert.Contains(t, resp.(*sshclient.PublicKey).Line, "ssh-ed25519")

	resp, err = handleSSHDeleteKey(ctx, svc, &sshclient.DeleteKeyRequest{
		Scope: "user", Owner: "alice", Name: "laptop",
	})
	require.NoError(t, err)
	assert.True(t, resp.(*sshclient.AckResponse).Success)
}

func TestEmbeddedSSHHandlers_ImportCarriesPassphrase(t *testing.T) {
	fake := sshfake.NewClient()
	svc := newSSHTestService(fake)

	resp, err := handleSSHImportKey(context.Background(), svc, &sshclient.ImportKeyRequest{
		Scope: "user", Owner: "alice", Name: "imported",
		PEM:           []byte("pem"),
		PassphraseB64: base64.StdEncoding.EncodeToString([]byte("s3cret")),
	})
	require.NoError(t, err)
	assert.Equal(t, "imported", resp.(*sshclient.Key).Name)
	assert.Equal(t, "s3cret", fake.LastImportPassphrase)
}

func TestEmbeddedSSHHandlers_SignDigest(t *testing.T) {
	fake := sshfake.NewClient()
	fake.SignerPublicKey = testEmbeddedSSHPublicKey(t)
	svc := newSSHTestService(fake)

	resp, err := handleSSHSignDigest(context.Background(), svc, &sshclient.SignRequest{
		Scope: "user", Owner: "alice", Name: "laptop", Digest: []byte("digest"),
	})
	require.NoError(t, err)
	sig, ok := resp.(*sshclient.SignResponse)
	require.True(t, ok)
	assert.Equal(t, "ssh-ed25519", sig.Format)
}

func TestEmbeddedSSHHandlers_KnownHostsAndAuthorized(t *testing.T) {
	fake := sshfake.NewClient()
	svc := newSSHTestService(fake)
	ctx := context.Background()
	host := sshfixture.Host(t)

	_, err := handleSSHAddKnownHost(ctx, svc, &sshclient.AddKnownHostRequest{
		Scope: "user", Owner: "alice", Host: "git.example.com",
		PublicKey: host.AuthorizedLine,
	})
	require.NoError(t, err)

	resp, err := handleSSHListKnownHosts(ctx, svc, &sshclient.ListKnownHostsRequest{Scope: "user", Owner: "alice"})
	require.NoError(t, err)
	pins := resp.(*sshclient.ListKnownHostsResponse).Items
	require.Len(t, pins, 1)
	assert.Equal(t, host.Fingerprint, pins[0].Fingerprint,
		"the pin must come back as the fingerprint of the key that was pinned")

	_, err = handleSSHRemoveKnownHost(ctx, svc, &sshclient.RemoveKnownHostRequest{
		Scope: "user", Owner: "alice", Host: "git.example.com",
	})
	require.NoError(t, err)

	_, err = handleSSHAddAuthorizedKey(ctx, svc, &sshclient.AddAuthorizedKeyRequest{
		Scope: "user", Owner: "alice", Name: "deploy", Line: "ssh-ed25519 AAAA",
	})
	require.NoError(t, err)

	resp, err = handleSSHListAuthorizedKeys(ctx, svc, &sshclient.ListAuthorizedKeysRequest{Scope: "user", Owner: "alice"})
	require.NoError(t, err)
	require.Len(t, resp.(*sshclient.ListAuthorizedKeysResponse).Items, 1)

	resp, err = handleSSHEmitAuthorizedKey(ctx, svc, &sshclient.EmitRequest{
		PublicKey: "ssh-ed25519 AAAA", Comment: "alice",
	})
	require.NoError(t, err)
	assert.Equal(t, "ssh-ed25519 AAAA alice", resp.(*sshclient.EmitResponse).Line)

	_, err = handleSSHRemoveAuthorizedKey(ctx, svc, &sshclient.RemoveAuthorizedKeyRequest{
		Scope: "user", Owner: "alice", Name: "deploy",
	})
	require.NoError(t, err)
}

// =============================================================================
// Dispatch registration
// =============================================================================

func TestEmbeddedSSHHandlers_AllMethodsRegistered(t *testing.T) {
	methods := []string{
		sshclient.MethodGenerateKey, sshclient.MethodImportKey, sshclient.MethodGetKey,
		sshclient.MethodListKeys, sshclient.MethodDeleteKey, sshclient.MethodExportPublicKey,
		sshclient.MethodSignDigest, sshclient.MethodAddKnownHost, sshclient.MethodListKnownHosts,
		sshclient.MethodRemoveKnownHost, sshclient.MethodAddAuthorizedKey,
		sshclient.MethodListAuthorizedKeys, sshclient.MethodRemoveAuthorizedKey,
		sshclient.MethodEmitAuthorizedKey,
	}
	for _, m := range methods {
		key := sshclient.ServiceName + "/" + m
		_, ok := unaryHandlers[key]
		assert.True(t, ok, "embedded transport is missing a handler for %s", key)
	}
}

// =============================================================================
// Error paths
// =============================================================================

func TestEmbeddedSSHHandlers_WrongRequestType(t *testing.T) {
	svc := newSSHTestService(sshfake.NewClient())
	ctx := context.Background()

	cases := []struct {
		name    string
		handler func(context.Context, service.QuicBaaS, interface{}) (interface{}, error)
	}{
		{"GenerateKey", handleSSHGenerateKey},
		{"ImportKey", handleSSHImportKey},
		{"GetKey", handleSSHGetKey},
		{"ListKeys", handleSSHListKeys},
		{"DeleteKey", handleSSHDeleteKey},
		{"ExportPublicKey", handleSSHExportPublicKey},
		{"SignDigest", handleSSHSignDigest},
		{"AddKnownHost", handleSSHAddKnownHost},
		{"ListKnownHosts", handleSSHListKnownHosts},
		{"RemoveKnownHost", handleSSHRemoveKnownHost},
		{"AddAuthorizedKey", handleSSHAddAuthorizedKey},
		{"ListAuthorizedKeys", handleSSHListAuthorizedKeys},
		{"RemoveAuthorizedKey", handleSSHRemoveAuthorizedKey},
		{"EmitAuthorizedKey", handleSSHEmitAuthorizedKey},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// A request from a different surface must be rejected by type,
			// not silently coerced.
			_, err := tc.handler(ctx, svc, &struct{ Nope string }{Nope: "wrong"})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "quicbaas.SSH")
		})
	}
}

func TestEmbeddedSSHHandlers_NilRequestRejected(t *testing.T) {
	svc := newSSHTestService(sshfake.NewClient())
	_, err := handleSSHGenerateKey(context.Background(), svc, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "<nil>")
}

func TestEmbeddedSSHHandlers_SSHNotConfigured(t *testing.T) {
	// SSHClient() returns nil => every verb reports not-configured.
	svc := &sshMockService{mockQuicBaaS: &mockQuicBaaS{}}
	_, err := handleSSHListKeys(context.Background(), svc, &sshclient.ListKeysRequest{})
	require.Error(t, err)
	var notConfigured *service.SSHNotConfiguredError
	assert.ErrorAs(t, err, &notConfigured)
}

func TestEmbeddedSSHHandlers_BackendErrorPropagates(t *testing.T) {
	backendErr := &xkmsssh.NotFoundError{Resource: "key", Message: "absent"}
	fake := sshfake.NewClient()
	fake.Err = backendErr
	svc := newSSHTestService(fake)

	_, err := handleSSHGetKey(context.Background(), svc, &sshclient.GetKeyRequest{Name: "absent"})
	require.ErrorIs(t, err, backendErr)
}

// testEmbeddedSSHPublicKey builds a throwaway ed25519 SSH public key.
func testEmbeddedSSHPublicKey(t *testing.T) gossh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	sshPub, err := gossh.NewPublicKey(pub)
	require.NoError(t, err)
	return sshPub
}
