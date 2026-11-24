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

package service

import (
	"context"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	qbcrypto "github.com/jeremyhahn/go-quicbaas/pkg/crypto"
	pkgerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// TestCrypto_RotatedTenantKeyVersionKeepsTheKeyPolicy checks, through the
// crypto service and the real KMS, that a principal the key policy denies
// cannot reach a rotated version of the key by naming it (k_v2) directly:
// the version has no policy of its own, so it must not resolve as a key.
func TestCrypto_RotatedTenantKeyVersionKeepsTheKeyPolicy(t *testing.T) {
	manager, cleanup := realKMSManagerForTest(t)
	defer cleanup()

	svc := newKMSService(manager, newMockLogger())
	WithKMSAuthzEnabled(true)(svc)
	svc.kvStore = newMockKVStore()

	cs := newCryptoService(qbcrypto.NewMemoryCryptoManager(manager), manager, newMockLogger(), nil)
	cs.kmsSvc = svc
	cs.authzEnabled = true

	admin := tenantctx.WithTenant(tenantCtx("acme"), "acme")
	bob := tenantctx.WithTenant(auth.SubjectIntoContext(context.Background(), interfaces.Subject{
		ID:            "user-bob",
		Username:      "bob",
		TenantID:      "acme",
		Authenticated: true,
	}), "acme")

	_, err := manager.GenerateKey(context.Background(), &xkmssdk.KeyAttributes{
		CN:                 "tenant/acme/k",
		StoreType:          "software",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	_, err = svc.SetKeyPolicy(admin, &SetKeyPolicyRequest{
		KeyID:  "k",
		Policy: &KeyPolicy{KeyID: "k", DeniedPrincipals: []string{"bob"}},
	})
	require.NoError(t, err)

	_, err = cs.Encrypt(admin, &EncryptRequest{KeyID: "k", Plaintext: []byte("x")})
	require.NoError(t, err)
	_, err = cs.Encrypt(bob, &EncryptRequest{KeyID: "k", Plaintext: []byte("x")})
	requireQBErrorCode(t, err, pkgerrors.ErrForbidden)

	_, err = manager.RotateKey(context.Background(), "tenant/acme/k")
	require.NoError(t, err)
	ct, err := cs.Encrypt(admin, &EncryptRequest{KeyID: "k", Plaintext: []byte("secret")})
	require.NoError(t, err)

	_, err = cs.Encrypt(bob, &EncryptRequest{KeyID: "k_v2", Plaintext: []byte("x")})
	require.Error(t, err, "the rotated version is not a key bob can use outside k's policy")
	_, err = cs.Decrypt(bob, &DecryptRequest{KeyID: "k_v2", Ciphertext: ct.Ciphertext})
	require.Error(t, err, "the rotated version is not a key bob can use outside k's policy")
	_, err = cs.Decrypt(bob, &DecryptRequest{KeyID: "k", Ciphertext: ct.Ciphertext})
	requireQBErrorCode(t, err, pkgerrors.ErrForbidden)

	pt, err := cs.Decrypt(admin, &DecryptRequest{KeyID: "k", Ciphertext: ct.Ciphertext})
	require.NoError(t, err)
	require.Equal(t, []byte("secret"), pt.Plaintext)
}
