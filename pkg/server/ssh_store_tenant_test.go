//go:build !integration

package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gossh "golang.org/x/crypto/ssh"

	qbshards "github.com/jeremyhahn/go-quicbaas/pkg/shards"
)

// newTenantSSHClient serves the SSH key store the way buildSSHClient does,
// over a real embedded node that runs the store as the service principal.
// A double cannot stand in: the property under test is that qrdb's
// self-scope KV check admits the store's calls for a non-system tenant.
func newTenantSSHClient(t *testing.T) xkmsssh.Client {
	t.Helper()
	s, _ := newPrincipalServer(t)
	shard, err := s.qrdbClient.ForShard(qbshards.PlatformShardID)
	require.NoError(t, err)
	backend, err := s.sshStoreBackend(shard.KV())
	require.NoError(t, err)
	client, err := newEmbeddedSSHClient(backend)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	return client
}

// sshTestPEM returns a fresh unencrypted OpenSSH ed25519 private key.
func sshTestPEM(t *testing.T) []byte {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	block, err := gossh.MarshalPrivateKey(priv, "")
	require.NoError(t, err)
	return pem.EncodeToMemory(block)
}

func sshKeyNames(keys []*xkmsssh.Key) []string {
	names := make([]string, 0, len(keys))
	for _, k := range keys {
		names = append(names, k.Name)
	}
	return names
}

func generateSSHKey(ctx context.Context, t *testing.T, client xkmsssh.Client, name string) *xkmsssh.Key {
	t.Helper()
	key, err := client.Keys().Generate(ctx, xkmsssh.GenerateRequest{
		Scope: "service", Owner: "deployer", Name: name, Algorithm: "ed25519",
	})
	require.NoError(t, err)
	return key
}

func TestSSHStore_TenantGeneratesImportsAndLists(t *testing.T) {
	client := newTenantSSHClient(t)
	ctx := qrdbsdk.ContextWithTenant(context.Background(), "tenant-a")

	generateSSHKey(ctx, t, client, "generated")
	_, err := client.Keys().Import(ctx, xkmsssh.ImportRequest{
		Scope: "service", Owner: "deployer", Name: "imported", PEM: sshTestPEM(t),
	}, nil)
	require.NoError(t, err)

	keys, err := client.Keys().List(ctx, xkmsssh.ListKeysRequest{})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"generated", "imported"}, sshKeyNames(keys))
}

func TestSSHStore_TenantCannotSeeAnotherTenantsKeys(t *testing.T) {
	client := newTenantSSHClient(t)
	ctxA := qrdbsdk.ContextWithTenant(context.Background(), "tenant-a")
	ctxB := qrdbsdk.ContextWithTenant(context.Background(), "tenant-b")
	ref := xkmsssh.GetKeyRequest{Scope: "service", Owner: "deployer", Name: "shared-name"}

	keyA := generateSSHKey(ctxA, t, client, ref.Name)

	listB, err := client.Keys().List(ctxB, xkmsssh.ListKeysRequest{})
	require.NoError(t, err)
	assert.Empty(t, listB, "tenant-b listed tenant-a's keys")
	_, err = client.Keys().Get(ctxB, ref)
	require.Error(t, err, "tenant-b read tenant-a's key")

	// The same name in tenant-b is tenant-b's own key, not an overwrite.
	keyB := generateSSHKey(ctxB, t, client, ref.Name)
	require.NotEqual(t, keyA.Fingerprint, keyB.Fingerprint)
	gotA, err := client.Keys().Get(ctxA, ref)
	require.NoError(t, err)
	assert.Equal(t, keyA.Fingerprint, gotA.Fingerprint, "tenant-b overwrote tenant-a's key")

	platform, err := client.Keys().List(context.Background(), xkmsssh.ListKeysRequest{})
	require.NoError(t, err)
	assert.Empty(t, platform, "the platform partition listed tenant keys")
}
