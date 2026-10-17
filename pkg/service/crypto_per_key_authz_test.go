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
	"encoding/json"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// perKeyAuthzFixture is a crypto service with per-key authorization enabled
// whose crypto and KMS manager doubles count every call that reaches them.
type perKeyAuthzFixture struct {
	svc   *cryptoService
	calls int
}

// newPerKeyAuthzFixture stores a policy on "authz-key" that allows only
// user-1, so user-1 is allowed and any other principal is denied.
func newPerKeyAuthzFixture(t *testing.T, allowedOps ...KeyUsage) *perKeyAuthzFixture {
	t.Helper()
	f := &perKeyAuthzFixture{}

	kmsManager := newMockKMSManager()
	kmsManager.keys["authz-key"] = &mockKeyData{keyID: "authz-key", status: "enabled"}
	kmsManager.generateDataKeyFn = func(_ context.Context, _ *xkmssdk.KeyAttributes, keyBytes int) ([]byte, []byte, error) {
		f.calls++
		return make([]byte, keyBytes), []byte("wrapped"), nil
	}

	cryptoMgr := newMockCryptoManager()
	cryptoMgr.rewrapFn = func(_ context.Context, _ string, ciphertext, _ []byte) ([]byte, error) {
		f.calls++
		return ciphertext, nil
	}
	cryptoMgr.encryptBatchFn = func(_ context.Context, _ string, items [][]byte, _ []byte) ([][]byte, error) {
		f.calls++
		return items, nil
	}
	cryptoMgr.decryptBatchFn = func(_ context.Context, _ string, items [][]byte, _ []byte) ([][]byte, error) {
		f.calls++
		return items, nil
	}
	cryptoMgr.generateHMACFn = func(_ context.Context, _ string, _ []byte, _ string) ([]byte, error) {
		f.calls++
		return []byte("mac"), nil
	}
	cryptoMgr.verifyHMACFn = func(_ context.Context, _ string, _, _ []byte, _ string) (bool, error) {
		f.calls++
		return true, nil
	}

	kmsSvc, kv := newAuthzEnabledKMS()
	kmsSvc.kmsManager = kmsManager
	data, err := json.Marshal(&KeyPolicy{
		KeyID:             "authz-key",
		AllowedPrincipals: []string{"user-1"},
		AllowedOperations: allowedOps,
	})
	require.NoError(t, err)
	require.NoError(t, kv.Put(systemTenantCtx(), kmsKeyPolicyKey("authz-key"), data))

	f.svc = newCryptoService(cryptoMgr, kmsManager, nil, NewAEADSafetyTracker())
	f.svc.kmsSvc = kmsSvc
	f.svc.authzEnabled = true
	return f
}

// TestCryptoService_PerKeyAuthz proves that each of the six verbs gates on
// the per-key policy: an allowed principal reaches the manager, a denied one
// gets ErrForbidden and never reaches it (#1480).
func TestCryptoService_PerKeyAuthz(t *testing.T) {
	verbs := []struct {
		name string
		call func(svc *cryptoService, ctx context.Context) error
	}{
		{"Rewrap", func(svc *cryptoService, ctx context.Context) error {
			_, err := svc.Rewrap(ctx, &RewrapRequest{KeyID: "authz-key", Ciphertext: []byte("ct")})
			return err
		}},
		{"EncryptBatch", func(svc *cryptoService, ctx context.Context) error {
			_, err := svc.EncryptBatch(ctx, &EncryptBatchRequest{
				KeyID: "authz-key",
				Items: []BatchEncryptItem{{Plaintext: []byte("pt")}},
			})
			return err
		}},
		{"DecryptBatch", func(svc *cryptoService, ctx context.Context) error {
			_, err := svc.DecryptBatch(ctx, &DecryptBatchRequest{
				KeyID: "authz-key",
				Items: []BatchDecryptItem{{Ciphertext: []byte("ct")}},
			})
			return err
		}},
		{"GenerateWrappedDataKey", func(svc *cryptoService, ctx context.Context) error {
			_, err := svc.GenerateWrappedDataKey(ctx, &GenerateWrappedDataKeyRequest{KeyID: "authz-key"})
			return err
		}},
		{"GenerateHMAC", func(svc *cryptoService, ctx context.Context) error {
			_, err := svc.GenerateHMAC(ctx, &HMACRequest{KeyID: "authz-key", Message: []byte("msg")})
			return err
		}},
		{"VerifyHMAC", func(svc *cryptoService, ctx context.Context) error {
			_, err := svc.VerifyHMAC(ctx, &VerifyHMACRequest{
				KeyID:   "authz-key",
				Message: []byte("msg"),
				HMAC:    []byte("mac"),
			})
			return err
		}},
	}

	for _, verb := range verbs {
		t.Run(verb.name+"/allowed", func(t *testing.T) {
			f := newPerKeyAuthzFixture(t)
			ctx := subjectCtx(authenticatedSubject("user-1", "alice"))

			require.NoError(t, verb.call(f.svc, ctx))
			assert.Equal(t, 1, f.calls)
		})

		t.Run(verb.name+"/denied", func(t *testing.T) {
			f := newPerKeyAuthzFixture(t)
			ctx := subjectCtx(authenticatedSubject("user-2", "bob"))

			err := verb.call(f.svc, ctx)
			var qe *errors.QBError
			require.ErrorAs(t, err, &qe)
			assert.Equal(t, errors.ErrForbidden, qe.Code)
			assert.Zero(t, f.calls)
		})
	}
}

// TestCryptoService_PerKeyAuthzOperations checks the operation each verb asks
// the key policy for: the HMAC verbs need the MAC operations, not sign/verify,
// and Rewrap needs both decrypt and encrypt on the key (#1488).
func TestCryptoService_PerKeyAuthzOperations(t *testing.T) {
	generateHMAC := func(svc *cryptoService, ctx context.Context) error {
		_, err := svc.GenerateHMAC(ctx, &HMACRequest{KeyID: "authz-key", Message: []byte("msg")})
		return err
	}
	verifyHMAC := func(svc *cryptoService, ctx context.Context) error {
		_, err := svc.VerifyHMAC(ctx, &VerifyHMACRequest{
			KeyID:   "authz-key",
			Message: []byte("msg"),
			HMAC:    []byte("mac"),
		})
		return err
	}
	rewrap := func(svc *cryptoService, ctx context.Context) error {
		_, err := svc.Rewrap(ctx, &RewrapRequest{KeyID: "authz-key", Ciphertext: []byte("ct")})
		return err
	}

	cases := []struct {
		name    string
		ops     []KeyUsage
		call    func(svc *cryptoService, ctx context.Context) error
		allowed bool
	}{
		{"GenerateHMAC/sign only", []KeyUsage{KeyUsageSign, KeyUsageVerify}, generateHMAC, false},
		{"GenerateHMAC/generate_mac", []KeyUsage{KeyUsageGenerateMAC}, generateHMAC, true},
		{"VerifyHMAC/verify only", []KeyUsage{KeyUsageSign, KeyUsageVerify}, verifyHMAC, false},
		{"VerifyHMAC/verify_mac", []KeyUsage{KeyUsageVerifyMAC}, verifyHMAC, true},
		{"Rewrap/no decrypt", []KeyUsage{KeyUsageEncrypt}, rewrap, false},
		{"Rewrap/no encrypt", []KeyUsage{KeyUsageDecrypt}, rewrap, false},
		{"Rewrap/decrypt and encrypt", []KeyUsage{KeyUsageDecrypt, KeyUsageEncrypt}, rewrap, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newPerKeyAuthzFixture(t, tc.ops...)
			ctx := subjectCtx(authenticatedSubject("user-1", "alice"))

			err := tc.call(f.svc, ctx)
			if tc.allowed {
				require.NoError(t, err)
				assert.Equal(t, 1, f.calls)
				return
			}
			var qe *errors.QBError
			require.ErrorAs(t, err, &qe)
			assert.Equal(t, errors.ErrForbidden, qe.Code)
			assert.Zero(t, f.calls)
		})
	}
}
