package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// earlyRotationTenant is a dedicated tenant whose ID sorts before the
// platform key's, so a lookup by common name alone finds its key first.
const earlyRotationTenant = "acme"

// systemRotation rotates the platform OIDC key on node a.
type systemRotation func(t *testing.T, a *iam.SigningKeyManager, kid string) error

var (
	managerSystemRotation systemRotation = func(_ *testing.T, a *iam.SigningKeyManager, _ string) error {
		return a.Rotate()
	}
	hostedSystemRotation systemRotation = func(t *testing.T, _ *iam.SigningKeyManager, kid string) error {
		return directHostedRotate.rotate(t, systemCtx(), kid)
	}
)

// requireSystemRotationRefusedBesideDedicatedKeys: with dedicated tenant keys
// sharing the platform key's common name in the replicated store, a system
// rotation cannot name the system-owned key exactly, so it is refused before
// anything is written: no record, key material or metadata moves, on either
// node, for the platform key or either tenant's.
func requireSystemRotationRefusedBesideDedicatedKeys(t *testing.T, rotate systemRotation) {
	a, b, kv, _, kid := hostedRotationNodes(t)
	tenants, err := iam.NewTenantSigningKeyResolver(a, dedicatedOIDCSettings{
		earlyRotationTenant:     true,
		dedicatedRotationTenant: true,
	})
	require.NoError(t, err)
	dbxkms.RegisterRotationDelegate(oidcSigningBackendName, oidcRotationDelegate(a, tenants))

	dedicated := make(map[string]string)
	for _, id := range []string{earlyRotationTenant, dedicatedRotationTenant} {
		m, err := tenants.ManagerFor(tenantctx.WithTenant(context.Background(), id), id)
		require.NoError(t, err)
		require.NotSame(t, a, m)
		dedicated[id], _, _ = m.Current()
	}
	before := snapshotKV(t, kv)
	g1, _, _ := a.Current()

	err = rotate(t, a, kid)
	var ambiguous *iam.SystemRotationAmbiguousError
	require.ErrorAs(t, err, &ambiguous)
	require.Equal(t, iam.OIDCSigningKeyCN, ambiguous.CN)
	require.Equal(t, []string{earlyRotationTenant, dedicatedRotationTenant}, ambiguous.Tenants)

	require.Equal(t, before, snapshotKV(t, kv), "a refused system rotation wrote to the shared store")
	g, _, _ := a.Current()
	require.Equal(t, g1, g, "the platform key rotated")
	require.NoError(t, b.Refresh())
	g, _, _ = b.Current()
	require.Equal(t, g1, g, "the shared platform record moved")
	for id, kid := range dedicated {
		m, err := tenants.ManagerFor(tenantctx.WithTenant(context.Background(), id), id)
		require.NoError(t, err)
		require.NoError(t, m.Refresh())
		current, _, _ := m.Current()
		require.Equal(t, kid, current, "tenant %s's key moved", id)
	}
}

func TestOIDCSigningStore_SystemRotationRefusedBesideDedicatedKeys(t *testing.T) {
	requireSystemRotationRefusedBesideDedicatedKeys(t, managerSystemRotation)
}

func TestOIDCSigningStore_HostedSystemRotationRefusedBesideDedicatedKeys(t *testing.T) {
	requireSystemRotationRefusedBesideDedicatedKeys(t, hostedSystemRotation)
}

// TestOIDCSigningStore_SystemRotationWithoutDedicatedKeys: with tenants
// configured for dedicated keys but none minted, the platform key is the only
// key with its common name, and a system rotation rotates it to v2 in the
// shared record.
func TestOIDCSigningStore_SystemRotationWithoutDedicatedKeys(t *testing.T) {
	a, b, _, _, _ := hostedRotationNodes(t)
	useNodeRotation(t, a)
	v1, _, _ := a.Current()

	require.NoError(t, a.Rotate())

	v2, _, _ := a.Current()
	require.NotEqual(t, v1, v2)
	require.NoError(t, b.Refresh())
	bCurrent, _, _ := b.Current()
	require.Equal(t, v2, bCurrent, "the shared record names A's v2")
}
