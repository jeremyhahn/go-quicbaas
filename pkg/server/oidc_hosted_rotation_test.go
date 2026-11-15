// Copyright (c) 2025 Jeremy Hahn

package server

import (
	"context"
	"testing"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// snapshotKV copies every record of a shared store: version records and key
// material alike.
func snapshotKV(t *testing.T, kv xkmssdk.StorageBackend) map[string]string {
	t.Helper()
	ctx := context.Background()
	keys, err := kv.List(ctx, "")
	require.NoError(t, err)
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		v, err := kv.Get(ctx, k)
		require.NoError(t, err)
		out[k] = string(v)
	}
	return out
}

// hostedRotationNodes boots two nodes on one replicated store and returns
// them with the store, its contents and the hosted key ID of the platform
// OIDC key.
func hostedRotationNodes(t *testing.T) (a, b *iam.SigningKeyManager, kv xkmssdk.StorageBackend, before map[string]string, kid string) {
	t.Helper()
	t.Cleanup(dbxkms.Reset)
	kv = xkmssdk.NewStorageMemory()
	a = bootOIDCSigningServer(t, &Server{config: &config.Config{}}, kv)
	b = bootOIDCSigningServer(t, &Server{config: &config.Config{}}, kv)
	return a, b, kv, snapshotKV(t, kv), oidcSigningBackendName + ":::" + iam.OIDCSigningKeyCN
}

// requireManagerRotates checks that SigningKeyManager.Rotate on b still
// rotates the key and that a picks the new version up.
func requireManagerRotates(t *testing.T, a, b *iam.SigningKeyManager) {
	t.Helper()
	v1, _, _ := b.Current()
	require.NoError(t, b.Rotate())
	v2, _, _ := b.Current()
	require.NotEqual(t, v1, v2)
	require.Eventually(t, func() bool {
		_, err := a.PublicForKID(v2)
		return err == nil
	}, 5*time.Second, 20*time.Millisecond)
}

// Two nodes share one replicated store. A hosted rotation of the platform
// OIDC key through this package's RotateKey on node B is refused with the
// typed error and leaves the shared version record and key material as they
// were; SigningKeyManager.Rotate still rotates it.
func TestOIDCSigningStore_HostedRotationRefusedOnPeer(t *testing.T) {
	a, b, kv, before, kid := hostedRotationNodes(t)

	_, err := dbxkms.RotateKey(context.Background(), kid)
	var refused *dbxkms.OIDCSigningKeyRotationRefusedError
	require.ErrorAs(t, err, &refused)
	require.Equal(t, before, snapshotKV(t, kv))

	requireManagerRotates(t, a, b)
}
