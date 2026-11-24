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

package kms

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// Two tenants each own a key of the same name. The name is the caller's name
// as given and the tenant is the key's TenantID, so neither tenant can see,
// use or delete the other's key.
func TestKMSTenantKeys_SameNameTwoTenants(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend := newRealXKMSBackendForTest(t)
	ctx := context.Background()
	acme := tenantctx.WithTenant(ctx, "acme")
	globex := tenantctx.WithTenant(ctx, "globex")
	const name = "orders-kek"

	for _, tctx := range []context.Context{acme, globex} {
		_, err := backend.CreateKey(tctx, backend.algorithmToKeyAttributes(name, AlgAES256GCM))
		require.NoError(t, err)
	}

	acmeCT, err := backend.Encrypt(acme, name, []byte("a"), nil)
	require.NoError(t, err)
	globexCT, err := backend.Encrypt(globex, name, []byte("g"), nil)
	require.NoError(t, err)

	_, err = backend.Decrypt(globex, name, acmeCT, nil)
	require.Error(t, err, "globex must not open acme's ciphertext")
	_, err = backend.Decrypt(acme, name, globexCT, nil)
	require.Error(t, err, "acme must not open globex's ciphertext")

	pt, err := backend.Decrypt(acme, name, acmeCT, nil)
	require.NoError(t, err)
	require.Equal(t, []byte("a"), pt)
	pt, err = backend.Decrypt(globex, name, globexCT, nil)
	require.NoError(t, err)
	require.Equal(t, []byte("g"), pt)

	for tenant, tctx := range map[string]context.Context{"acme": acme, "globex": globex} {
		keys, _, err := backend.ListKeys(tctx, "", 0)
		require.NoError(t, err)
		require.Len(t, keys, 1, "tenant %s lists only its own key", tenant)
		require.Equal(t, name, keys[0].KeyID)
		require.Equal(t, tenant, keys[0].TenantID)
	}

	systemKeys, _, err := backend.ListKeys(tenantctx.WithSystem(ctx), "", 0)
	require.NoError(t, err)
	for _, k := range systemKeys {
		require.NotEqual(t, name, k.KeyID, "the system tenant lists no tenant's key")
	}

	require.NoError(t, backend.DeleteKey(acme, name))
	_, err = backend.GetKeyMetadata(acme, name)
	require.Error(t, err, "acme's key is gone")

	globexCT, err = backend.Encrypt(globex, name, []byte("g2"), nil)
	require.NoError(t, err, "globex's key survives acme's delete")
	pt, err = backend.Decrypt(globex, name, globexCT, nil)
	require.NoError(t, err)
	require.Equal(t, []byte("g2"), pt)
}
