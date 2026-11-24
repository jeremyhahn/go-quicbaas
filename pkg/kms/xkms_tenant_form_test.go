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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// A tenant key created under a tenant ctx is stored in go-xkms as CN=<name>
// with TenantID=<tenant>: the name carries no tenant and no encoding.
func TestXKMSTenantForm_CreateStoresCNAndTenantID(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend := newRealXKMSBackendForTest(t)
	acme := tenantctx.WithTenant(context.Background(), "acme")

	meta, err := backend.CreateKey(acme, backend.algorithmToKeyAttributes("orders-kek", AlgAES256GCM))
	require.NoError(t, err)
	require.Equal(t, "orders-kek", meta.KeyID)
	require.Equal(t, "acme", meta.TenantID)

	keys, err := xkms.ListKeys(context.Background(), string(backend.backendType))
	require.NoError(t, err)
	var found bool
	for _, attrs := range keys {
		if attrs == nil {
			continue
		}
		require.False(t, strings.HasPrefix(attrs.CN, "qbx."), "no CN is encoded: %s", attrs.CN)
		require.False(t, strings.HasPrefix(attrs.CN, "tenant/"), "no CN names a tenant: %s", attrs.CN)
		if attrs.CN == "orders-kek" && attrs.TenantID == "acme" {
			found = true
		}
	}
	require.True(t, found, "the key is stored as CN=orders-kek TenantID=acme")
}

// A key go-xkms holds natively as CN=<name> TenantID=<tenant> is the tenant's
// key of that name: the tenant finds, uses and lists it, another tenant does
// not find it.
func TestXKMSTenantForm_NativeTenantKeyResolves(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend := newRealXKMSBackendForTest(t)
	ctx := context.Background()
	attrs := backend.algorithmToKeyAttributes("native-k", AlgAES256GCM)
	attrs.TenantID = "acme"
	attrs.KeyType = symmetricKeyType
	_, err := xkms.GenerateSymmetricKey(ctx, string(backend.backendType), attrs)
	require.NoError(t, err)

	acme := tenantctx.WithTenant(ctx, "acme")
	ct, err := backend.Encrypt(acme, "native-k", []byte("hello"), nil)
	require.NoError(t, err)
	pt, err := backend.Decrypt(acme, "native-k", ct, nil)
	require.NoError(t, err)
	require.Equal(t, []byte("hello"), pt)

	keys, _, err := backend.ListKeys(acme, "", 0)
	require.NoError(t, err)
	var names []string
	for _, k := range keys {
		names = append(names, k.KeyID)
	}
	require.Contains(t, names, "native-k")

	_, err = backend.GetKeyMetadata(tenantctx.WithTenant(ctx, "globex"), "native-k")
	require.Error(t, err, "native-k is not found under the globex ctx")
}

// A tenant's key is created, used, rotated, used again, listed and deleted
// by its bare name under the tenant's ctx.
func TestXKMSTenantForm_EncryptRotateListDelete(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend := newRealXKMSBackendForTest(t)
	ctx := tenantctx.WithTenant(context.Background(), "acme")
	const keyID = "crypto-key"

	_, err := backend.CreateKey(ctx, backend.algorithmToKeyAttributes(keyID, AlgAES256GCM))
	require.NoError(t, err)

	ct, err := backend.Encrypt(ctx, keyID, []byte("secret"), nil)
	require.NoError(t, err)
	pt, err := backend.Decrypt(ctx, keyID, ct, nil)
	require.NoError(t, err)
	require.Equal(t, []byte("secret"), pt)

	_, err = backend.RotateKey(ctx, keyID)
	require.NoError(t, err)

	ct, err = backend.Encrypt(ctx, keyID, []byte("after"), nil)
	require.NoError(t, err)
	pt, err = backend.Decrypt(ctx, keyID, ct, nil)
	require.NoError(t, err)
	require.Equal(t, []byte("after"), pt)

	keys, _, err := backend.ListKeys(ctx, "", 0)
	require.NoError(t, err)
	var names []string
	for _, k := range keys {
		names = append(names, k.KeyID)
	}
	require.Contains(t, names, keyID)

	require.NoError(t, backend.DeleteKey(ctx, keyID))
}

// A name cannot spell a tenant: "tenant/acme/k" is refused as invalid input.
func TestXKMSTenantForm_SlashNameRefused(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend := newRealXKMSBackendForTest(t)
	ctx := context.Background()

	_, err := backend.CreateKey(ctx, backend.algorithmToKeyAttributes("tenant/acme/k", AlgAES256GCM))
	require.Error(t, err)
	require.True(t, qberrors.IsInvalidInput(err), "CreateKey: %v", err)

	_, err = backend.GetKeyMetadata(ctx, "tenant/acme/k")
	require.Error(t, err)
	require.True(t, qberrors.IsInvalidInput(err), "GetKeyMetadata: %v", err)
}
