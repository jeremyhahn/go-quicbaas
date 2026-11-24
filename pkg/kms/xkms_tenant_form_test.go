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

	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// A tenant key is stored in go-xkms's own form: CN is the key name and the
// tenant is the TenantID attribute. Nothing is encoded into the name.
func TestXKMSTenantForm_CreateStoresCNAndTenantID(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend := newRealXKMSBackendForTest(t)
	ctx := context.Background()

	_, err := backend.CreateKey(ctx, backend.algorithmToKeyAttributes("tenant/acme/orders-kek", AlgAES256GCM))
	require.NoError(t, err)

	stored, err := xkms.ListKeys(ctx, string(backend.backendType))
	require.NoError(t, err)
	found := false
	for _, attrs := range stored {
		require.False(t, strings.HasPrefix(attrs.CN, "qbx."), "no encoded name is stored: %q", attrs.CN)
		require.NotContains(t, attrs.CN, "tenant/", "no reference is stored as a CN")
		if attrs.CN == "orders-kek" && attrs.TenantID == "acme" {
			found = true
		}
	}
	require.True(t, found, "the key is stored as CN=orders-kek TenantID=acme")
}

// A key written natively by another transport (CN=K, TenantID=T) is the same
// key as the reference tenant/T/K, and only for tenant T.
func TestXKMSTenantForm_NativeTenantKeyResolves(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend := newRealXKMSBackendForTest(t)
	ctx := context.Background()

	attrs := backend.algorithmToKeyAttributes("native-k", AlgAES256GCM)
	attrs.TenantID = "acme"
	_, err := xkms.GenerateSymmetricKey(ctx, string(backend.backendType), attrs)
	require.NoError(t, err)

	const ref = "tenant/acme/native-k"
	ciphertext, err := backend.Encrypt(ctx, ref, []byte("secret"), nil)
	require.NoError(t, err)
	plaintext, err := backend.Decrypt(ctx, ref, ciphertext, nil)
	require.NoError(t, err)
	require.Equal(t, []byte("secret"), plaintext)

	keys, _, err := backend.ListKeys(ctx, "tenant/acme/", 0)
	require.NoError(t, err)
	var listed []string
	for _, k := range keys {
		listed = append(listed, k.KeyID)
	}
	require.Contains(t, listed, ref)

	_, err = backend.GetKeyMetadata(ctx, "tenant/globex/native-k")
	require.Error(t, err, "another tenant must not reach the key")
}

// A literal platform name that looks like the old encoding is a platform key,
// never a tenant key.
// Upgrade fixture: a key stored under the row's interim encoding is counted as
// a legacy tenant key; a literal qbx name and a new-form key are not.
