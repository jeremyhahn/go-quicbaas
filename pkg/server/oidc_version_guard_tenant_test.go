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
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// TestOIDCSigningStore_TenantIDShapedLikeAVersionCreatesItsFirstKey proves the
// unfenced-write guard reads only a key's reserved version suffix: a dedicated
// tenant whose id ends like a rotation version ("tenant--v2", a valid tenant
// id) creates its first signing key on the replicated backend with no fence,
// and its first rotation (a real version name, created under the fence) still
// succeeds.
func TestOIDCSigningStore_TenantIDShapedLikeAVersionCreatesItsFirstKey(t *testing.T) {
	t.Cleanup(dbxkms.Reset)
	const tenantID = "tenant--v2"
	initOIDCSigningDefault(t)
	registerOIDCSigningBackend(t, xkmssdk.NewStorageMemory())

	m, err := iam.NewSigningKeyManagerFromXKMSBackend(iam.OIDCSigningKeyCN, tenantID, oidcSigningBackendName)
	require.NoError(t, err, "the tenant's first signing key")
	kid, _, _ := m.Current()
	require.NotEmpty(t, kid)
	require.NoError(t, m.Rotate(), "the tenant's first rotation")
	rotated, _, _ := m.Current()
	require.NotEqual(t, kid, rotated)

	// On the store itself: the version suffix is read from the common-name
	// segment only, so real version names stay refused without the fence.
	keys, err := newOIDCKeyMaterialStore(xkmssdk.NewStorageMemory())
	require.NoError(t, err)
	ctx := context.Background()
	ok, err := keys.PutIfAbsent(ctx, "keys/"+tenantID+":software:signing:oidc-signing:ecdsa.key", []byte("m1"))
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, keys.Put(ctx, "meta/"+tenantID+":software:signing:oidc-signing:ecdsa", []byte("a1")))
	var unfenced *OIDCRotationUnfencedError
	for _, version := range []string{
		"keys/" + tenantID + ":software:signing:oidc-signing--v3:ecdsa.key",
		"keys/software:signing:oidc-signing--v2:ecdsa.key",
		"meta/acme:software:signing:oidc-signing--v2:ecdsa",
	} {
		_, err := keys.PutIfAbsent(ctx, version, []byte("m"))
		require.ErrorAs(t, err, &unfenced, version)
		require.ErrorAs(t, keys.Put(ctx, version, []byte("m")), &unfenced, version)
	}
}
