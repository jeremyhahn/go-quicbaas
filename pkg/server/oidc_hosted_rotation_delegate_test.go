// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// hostedRotate is one transport's hosted rotation of kid (the REST or MCP
// xkms rotate route, or dbxkms.RotateKey) for a caller whose context is ctx.
// typed reports whether its errors keep their types (REST's are its JSON
// body's text).
type hostedRotate struct {
	rotate func(t *testing.T, ctx context.Context, kid string) error
	typed  bool
}

var directHostedRotate = hostedRotate{
	rotate: func(_ *testing.T, ctx context.Context, kid string) error {
		_, err := dbxkms.RotateKey(ctx, kid)
		return err
	},
	typed: true,
}

const dedicatedRotationTenant = "tenant-dedicated"

// useNodeRotation registers node m's hosted rotation, as its startup does:
// in-process nodes share one registry, so the node a test registers last is
// the one whose route serves the next hosted rotation.
func useNodeRotation(t *testing.T, m *iam.SigningKeyManager) *iam.TenantSigningKeyResolver {
	t.Helper()
	tenants, err := iam.NewTenantSigningKeyResolver(m, dedicatedOIDCSettings{dedicatedRotationTenant: true})
	require.NoError(t, err)
	dbxkms.RegisterRotationDelegate(oidcSigningBackendName, oidcRotationDelegate(m, tenants))
	return tenants
}

// takeOIDCRotationLease takes tenantID's OIDC signing key rotation lease, as
// a node mid-rotation holds it, and shortens the wait for it so a contending
// rotation gives up at once. The returned function releases it.
func takeOIDCRotationLease(t *testing.T, kv xkmssdk.StorageBackend, tenantID string) func() {
	t.Helper()
	wait := oidcRotationLeaseWait
	oidcRotationLeaseWait = 100 * time.Millisecond
	t.Cleanup(func() { oidcRotationLeaseWait = wait })
	guarded, err := guardedOIDCStore(kv)
	require.NoError(t, err)
	versions, err := xkmssdk.NewStoragePrefixBackend(kv, oidcSigningVersionsPrefix)
	require.NoError(t, err)
	guardedVersions, err := guardedOIDCStore(versions)
	require.NoError(t, err)
	_, unlock, err := newOIDCRotationLock(guarded, guardedVersions)(context.Background(), tenantID, iam.OIDCSigningKeyCN)
	require.NoError(t, err)
	return unlock
}

func systemCtx() context.Context { return tenantctx.WithSystem(context.Background()) }

// requireHostedRotationRotatesThroughLease: an authorized hosted rotation of
// the platform OIDC key on node A makes v2 current in the shared record, and
// node B verifies a token A signs with v2.
func requireHostedRotationRotatesThroughLease(t *testing.T, via hostedRotate) {
	a, b, _, _, kid := hostedRotationNodes(t)
	useNodeRotation(t, a)
	v1, _, _ := a.Current()

	require.NoError(t, via.rotate(t, systemCtx(), kid))

	v2, signer, _ := a.Current()
	require.NotEqual(t, v1, v2)
	require.NoError(t, b.Refresh())
	bCurrent, _, _ := b.Current()
	require.Equal(t, v2, bCurrent, "the shared record names A's v2")

	tok, err := dbxkms.JWTSignWithSignerKID(signer, dbxkms.JWTClaims{
		"sub": "hosted-rotation",
		"exp": time.Now().Add(time.Hour).Unix(),
	}, iam.SigningAlgorithmES256, v2)
	require.NoError(t, err)
	pub, err := b.PublicForKID(v2)
	require.NoError(t, err)
	_, err = dbxkms.JWTVerifyWithKeyAndClaims(tok, pub, iam.SigningAlgorithmES256)
	require.NoError(t, err)
}

// requireConcurrentHostedRotationLosesLease: node B's hosted rotation while
// A holds the lease returns the typed busy error, leaves nothing it
// generated, and the record stays A's.
func requireConcurrentHostedRotationLosesLease(t *testing.T, via hostedRotate) {
	a, b, kv, _, kid := hostedRotationNodes(t)
	useNodeRotation(t, a)
	require.NoError(t, via.rotate(t, systemCtx(), kid))
	aCurrent, _, _ := a.Current()

	unlock := takeOIDCRotationLease(t, kv, "")
	before := snapshotKV(t, kv)
	useNodeRotation(t, b)
	err := via.rotate(t, systemCtx(), kid)
	require.Error(t, err)
	if via.typed {
		require.ErrorAs(t, err, new(*OIDCRotationBusyError))
	} else {
		require.Contains(t, err.Error(), "rotation lease")
	}
	require.Equal(t, before, snapshotKV(t, kv), "B's lost rotation left nothing behind")
	unlock()

	require.NoError(t, b.Refresh())
	bCurrent, _, _ := b.Current()
	require.Equal(t, aCurrent, bCurrent, "the record stays A's")
}

// requireUnauthorizedHostedRotationRefused: a caller acting for a tenant with
// no dedicated key, or for no tenant at all, rotates nothing.
func requireUnauthorizedHostedRotationRefused(t *testing.T, via hostedRotate) {
	a, _, kv, before, kid := hostedRotationNodes(t)
	useNodeRotation(t, a)
	v1, _, _ := a.Current()

	err := via.rotate(t, tenantctx.WithTenant(context.Background(), "tenant-plain"), kid)
	want := &OIDCSigningKeyNotDedicatedError{TenantID: "tenant-plain"}
	require.Error(t, err)
	if via.typed {
		require.ErrorAs(t, err, new(*OIDCSigningKeyNotDedicatedError))
	}
	require.Contains(t, err.Error(), want.Error())

	err = via.rotate(t, context.Background(), kid)
	_, missing := tenantctx.FromContext(context.Background(), oidcRotationOp)
	require.Error(t, err)
	if via.typed {
		require.ErrorAs(t, err, new(*tenantctx.MissingTenantError))
	}
	require.Contains(t, err.Error(), missing.Error())

	require.Equal(t, before, snapshotKV(t, kv))
	v, _, _ := a.Current()
	require.Equal(t, v1, v)
}

// requireDedicatedTenantKeyRotates: a dedicated tenant's replicated key
// rotates through the same route, and the platform key does not move.
func requireDedicatedTenantKeyRotates(t *testing.T, via hostedRotate) {
	a, b, _, _, kid := hostedRotationNodes(t)
	tenants := useNodeRotation(t, a)
	ctx := tenantctx.WithTenant(context.Background(), dedicatedRotationTenant)
	dedicated, err := tenants.ManagerFor(ctx, dedicatedRotationTenant)
	require.NoError(t, err)
	require.NotSame(t, a, dedicated)
	d1, _, _ := dedicated.Current()
	g1, _, _ := a.Current()

	require.NoError(t, via.rotate(t, ctx, kid))

	d2, _, _ := dedicated.Current()
	require.NotEqual(t, d1, d2)
	g, _, _ := a.Current()
	require.Equal(t, g1, g, "the platform key did not rotate")
	require.NoError(t, b.Refresh())
	g, _, _ = b.Current()
	require.Equal(t, g1, g, "the shared platform record did not move")
}

func TestOIDCSigningStore_HostedRotationRotatesThroughLease(t *testing.T) {
	requireHostedRotationRotatesThroughLease(t, directHostedRotate)
}

func TestOIDCSigningStore_ConcurrentHostedRotationLosesLease(t *testing.T) {
	requireConcurrentHostedRotationLosesLease(t, directHostedRotate)
}

func TestOIDCSigningStore_UnauthorizedHostedRotationRefused(t *testing.T) {
	requireUnauthorizedHostedRotationRefused(t, directHostedRotate)
}

func TestOIDCSigningStore_DedicatedTenantHostedRotation(t *testing.T) {
	requireDedicatedTenantKeyRotates(t, directHostedRotate)
}
