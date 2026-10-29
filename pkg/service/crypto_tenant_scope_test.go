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

	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
)

// cryptoScopeVerb invokes one crypto verb against the key named keyID.
type cryptoScopeVerb func(svc *cryptoService, ctx context.Context, keyID string) error

// ownerKeyOnly mirrors the production key store: only the owner tenant's
// scoped key exists, so any other id is reported as kms.ErrKeyNotFound.
func ownerKeyOnly(keyID string) error {
	if keyID != tenantScopedKeyID(scopeOwnerTenant, scopeKeyName) {
		return kms.ErrKeyNotFound
	}
	return nil
}

// newTenantScopeCrypto returns a crypto service whose backends hold one key,
// owned by scopeOwnerTenant under its tenant-scoped id.
func newTenantScopeCrypto(t *testing.T) *cryptoService {
	t.Helper()
	cryptoMgr := &mockCryptoManager{
		encryptFn: func(_ context.Context, keyID string, plaintext, _ []byte) ([]byte, error) {
			return plaintext, ownerKeyOnly(keyID)
		},
		decryptFn: func(_ context.Context, keyID string, ciphertext, _ []byte) ([]byte, error) {
			return ciphertext, ownerKeyOnly(keyID)
		},
		rewrapFn: func(_ context.Context, keyID string, ciphertext, _ []byte) ([]byte, error) {
			return ciphertext, ownerKeyOnly(keyID)
		},
		encryptBatchFn: func(_ context.Context, keyID string, items [][]byte, _ []byte) ([][]byte, error) {
			return items, ownerKeyOnly(keyID)
		},
		decryptBatchFn: func(_ context.Context, keyID string, items [][]byte, _ []byte) ([][]byte, error) {
			return items, ownerKeyOnly(keyID)
		},
		signFn: func(_ context.Context, keyID string, input []byte, _ string, _ bool) ([]byte, error) {
			return input, ownerKeyOnly(keyID)
		},
		verifyFn: func(_ context.Context, keyID string, _, _ []byte, _ string) (bool, error) {
			return true, ownerKeyOnly(keyID)
		},
		generateHMACFn: func(_ context.Context, keyID string, input []byte, _ string) ([]byte, error) {
			return input, ownerKeyOnly(keyID)
		},
		verifyHMACFn: func(_ context.Context, keyID string, _, _ []byte, _ string) (bool, error) {
			return true, ownerKeyOnly(keyID)
		},
	}
	kmsMgr := newMockKMSManager()
	kmsMgr.generateDataKeyFn = func(_ context.Context, attrs *xkmssdk.KeyAttributes, keyBytes int) ([]byte, []byte, error) {
		if err := ownerKeyOnly(attrs.CN); err != nil {
			return nil, nil, err
		}
		return make([]byte, keyBytes), []byte("wrapped"), nil
	}
	svc := newCryptoService(cryptoMgr, kmsMgr, nil, nil)
	svc.kmsSvc = newKMSService(kmsMgr, nil)
	seedTenantKeyOwners(t, svc.kmsSvc, scopeOwnerTenant, scopeKeyName)
	return svc
}

// cryptoScopeVerbs lists every crypto verb that addresses a key.
func cryptoScopeVerbs() map[string]cryptoScopeVerb {
	data := []byte("data")
	return map[string]cryptoScopeVerb{
		"Encrypt": func(svc *cryptoService, ctx context.Context, keyID string) error {
			_, err := svc.Encrypt(ctx, &EncryptRequest{KeyID: keyID, Plaintext: data})
			return err
		},
		"Decrypt": func(svc *cryptoService, ctx context.Context, keyID string) error {
			_, err := svc.Decrypt(ctx, &DecryptRequest{KeyID: keyID, Ciphertext: data})
			return err
		},
		"Rewrap": func(svc *cryptoService, ctx context.Context, keyID string) error {
			_, err := svc.Rewrap(ctx, &RewrapRequest{KeyID: keyID, Ciphertext: data})
			return err
		},
		"EncryptBatch": func(svc *cryptoService, ctx context.Context, keyID string) error {
			_, err := svc.EncryptBatch(ctx, &EncryptBatchRequest{KeyID: keyID, Items: []BatchEncryptItem{{Plaintext: data}}})
			return err
		},
		"DecryptBatch": func(svc *cryptoService, ctx context.Context, keyID string) error {
			_, err := svc.DecryptBatch(ctx, &DecryptBatchRequest{KeyID: keyID, Items: []BatchDecryptItem{{Ciphertext: data}}})
			return err
		},
		"GenerateDataKey": func(svc *cryptoService, ctx context.Context, keyID string) error {
			_, err := svc.GenerateDataKey(ctx, &GenerateDataKeyRequest{KeyID: keyID})
			return err
		},
		"GenerateWrappedDataKey": func(svc *cryptoService, ctx context.Context, keyID string) error {
			_, err := svc.GenerateWrappedDataKey(ctx, &GenerateWrappedDataKeyRequest{KeyID: keyID})
			return err
		},
		"Sign": func(svc *cryptoService, ctx context.Context, keyID string) error {
			_, err := svc.Sign(ctx, &SignRequest{KeyID: keyID, Message: data})
			return err
		},
		"Verify": func(svc *cryptoService, ctx context.Context, keyID string) error {
			_, err := svc.Verify(ctx, &VerifyRequest{KeyID: keyID, Message: data, Signature: data})
			return err
		},
		"GenerateHMAC": func(svc *cryptoService, ctx context.Context, keyID string) error {
			_, err := svc.GenerateHMAC(ctx, &HMACRequest{KeyID: keyID, Message: data})
			return err
		},
		"VerifyHMAC": func(svc *cryptoService, ctx context.Context, keyID string) error {
			_, err := svc.VerifyHMAC(ctx, &VerifyHMACRequest{KeyID: keyID, Message: data, HMAC: data})
			return err
		},
	}
}

// TestCryptoService_EveryKeyVerbIsTenantScoped proves that each crypto verb
// resolves the caller's key id under the caller's tenant: the owner reaches
// its key, another tenant naming the same key gets not-found, and a context
// carrying no tenant is refused.
func TestCryptoService_EveryKeyVerbIsTenantScoped(t *testing.T) {
	cases := tenantScopeCases()
	for verbName, verb := range cryptoScopeVerbs() {
		for _, tc := range cases {
			t.Run(verbName+"/"+tc.name, func(t *testing.T) {
				svc := newTenantScopeCrypto(t)
				tc.check(t, verb(svc, tc.ctx, scopeKeyName))
			})
		}
	}
}
