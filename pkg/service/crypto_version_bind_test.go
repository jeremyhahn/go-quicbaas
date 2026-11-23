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
	stderrors "errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	kctypes "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
)

// sealAtFixtureVersion seals plaintext as the fixture does: EncryptedData
// JSON recording the fixture's current version.
func sealAtFixtureVersion(f *versionedCryptoFixture, plaintext []byte) ([]byte, error) {
	return json.Marshal(kctypes.EncryptedData{
		Ciphertext: plaintext,
		Metadata:   map[string][]byte{"kv": []byte(fmt.Sprintf("%d", f.version.Load()))},
	})
}

// rotateDuringFirstSeal replaces the fixture's sealers (Encrypt, EncryptBatch
// and data-key generation) with ones that, on their first call only, rotate
// the key to version 2 before sealing (and with disableV2 also disable v2),
// as a rotation on another node landing between the status check and the
// seal would. It returns the number of seal calls.
func rotateDuringFirstSeal(f *versionedCryptoFixture, disableV2 bool) *atomic.Int64 {
	calls := &atomic.Int64{}
	before := func() {
		if calls.Add(1) != 1 {
			return
		}
		f.version.Store(2)
		f.kmsMgr.keys[versionStatusKeyID].version = 2
		if disableV2 {
			now := time.Now()
			f.kmsSvc.setVersionStatus(f.scoped, 2, KeyStatusDisabled, &now)
		}
	}
	cryptoMgr := f.svc.cryptoManager.(*mockCryptoManager)
	cryptoMgr.encryptFn = func(_ context.Context, _ string, plaintext, _ []byte) ([]byte, error) {
		before()
		return sealAtFixtureVersion(f, plaintext)
	}
	cryptoMgr.encryptBatchFn = func(_ context.Context, _ string, items [][]byte, _ []byte) ([][]byte, error) {
		before()
		out := make([][]byte, len(items))
		for i, item := range items {
			ct, err := sealAtFixtureVersion(f, item)
			if err != nil {
				return nil, err
			}
			out[i] = ct
		}
		return out, nil
	}
	f.kmsMgr.generateDataKeyFn = func(_ context.Context, _ *kctypes.KeyAttributes, keyBytes int) ([]byte, []byte, error) {
		before()
		plaintext := make([]byte, keyBytes)
		wrapped, err := sealAtFixtureVersion(f, plaintext)
		return plaintext, wrapped, err
	}
	return calls
}

// bindCase is one encrypting operation: run returns the ciphertexts it
// produced and, when the response reports one, its key version.
type bindCase struct {
	name string
	ops  uint64
	run  func(f *versionedCryptoFixture) (out [][]byte, keyVersion *uint64, err error)
}

func bindCases() []bindCase {
	ctx := systemTenantCtx()
	return []bindCase{
		{name: "Encrypt", ops: 1, run: func(f *versionedCryptoFixture) ([][]byte, *uint64, error) {
			resp, err := f.svc.Encrypt(ctx, &EncryptRequest{KeyID: versionStatusKeyID, Plaintext: []byte("p")})
			if err != nil {
				return nil, nil, err
			}
			return [][]byte{resp.Ciphertext}, &resp.KeyVersion, nil
		}},
		{name: "EncryptBatch", ops: 2, run: func(f *versionedCryptoFixture) ([][]byte, *uint64, error) {
			resp, err := f.svc.EncryptBatch(ctx, &EncryptBatchRequest{
				KeyID: versionStatusKeyID,
				Items: []BatchEncryptItem{{Plaintext: []byte("a")}, {Plaintext: []byte("b")}},
			})
			if err != nil {
				return nil, nil, err
			}
			out := make([][]byte, len(resp.Results))
			for i, r := range resp.Results {
				out[i] = r.Ciphertext
			}
			return out, &resp.KeyVersion, nil
		}},
		{name: "GenerateDataKey", ops: 1, run: func(f *versionedCryptoFixture) ([][]byte, *uint64, error) {
			resp, err := f.svc.GenerateDataKey(ctx, &GenerateDataKeyRequest{KeyID: versionStatusKeyID})
			if err != nil {
				return nil, nil, err
			}
			return [][]byte{resp.CiphertextBlob}, nil, nil
		}},
		{name: "GenerateWrappedDataKey", ops: 1, run: func(f *versionedCryptoFixture) ([][]byte, *uint64, error) {
			resp, err := f.svc.GenerateWrappedDataKey(ctx, &GenerateWrappedDataKeyRequest{KeyID: versionStatusKeyID})
			if err != nil {
				return nil, nil, err
			}
			return [][]byte{resp.CiphertextBlob}, nil, nil
		}},
	}
}

// TestCryptoService_RotationBetweenCheckAndSeal_SealsUnderCheckedVersion:
// another node rotates the key from v1 to v2 after the operation checked v1
// and counted its use, but before the backend seals, so the backend seals
// under v2. The v2 output is discarded, its use is counted against v2, and
// the retry checks, counts and seals under v2: every returned ciphertext is
// sealed under the version that was checked.
func TestCryptoService_RotationBetweenCheckAndSeal_SealsUnderCheckedVersion(t *testing.T) {
	for _, tc := range bindCases() {
		t.Run(tc.name, func(t *testing.T) {
			f := newVersionedCryptoFixture(t)
			tracker := NewAEADSafetyTracker()
			f.svc.safetyTracker = tracker
			calls := rotateDuringFirstSeal(f, false)

			out, keyVersion, err := tc.run(f)
			require.NoError(t, err)
			require.NotEmpty(t, out)
			for _, ct := range out {
				v, err := kms.CiphertextKeyVersion(ct)
				require.NoError(t, err)
				require.Equal(t, uint64(2), v, "the output is sealed under the checked version")
			}
			if keyVersion != nil {
				require.Equal(t, uint64(2), *keyVersion)
			}
			require.Equal(t, int64(2), calls.Load(), "the output sealed under an unchecked version is discarded and the seal retried")
			ops, _, _ := tracker.GetKeyUsage(f.scoped)
			require.Equal(t, 2*tc.ops, ops, "the discarded and the returned use are both counted on v2")

			_, err = f.svc.Encrypt(systemTenantCtx(), &EncryptRequest{KeyID: versionStatusKeyID, Plaintext: []byte("q")})
			require.NoError(t, err)
			after, _, _ := tracker.GetKeyUsage(f.scoped)
			require.Equal(t, ops+1, after, "a later use adds to v2's usage, it does not reset it")
		})
	}
}

// TestCryptoService_RotationToDisabledVersionBetweenCheckAndSeal_IsRefused:
// the rotation that lands between the check of v1 and the seal moves the key
// to a v2 that is disabled. The v2 output is discarded and the retry's status
// check refuses v2, so no ciphertext is returned.
func TestCryptoService_RotationToDisabledVersionBetweenCheckAndSeal_IsRefused(t *testing.T) {
	for _, tc := range bindCases() {
		t.Run(tc.name, func(t *testing.T) {
			f := newVersionedCryptoFixture(t)
			f.svc.safetyTracker = NewAEADSafetyTracker()
			rotateDuringFirstSeal(f, true)

			out, _, err := tc.run(f)
			requireKeyDisabled(t, err)
			require.Empty(t, out)
		})
	}
}

// TestCryptoService_KeyVersionMovingOnEveryAttempt_IsUnavailable: the key is
// rotated between the check and the seal of every attempt, so no attempt
// seals under the version it checked; the operation fails as unavailable
// after sealVersionAttempts attempts.
func TestCryptoService_KeyVersionMovingOnEveryAttempt_IsUnavailable(t *testing.T) {
	f := newVersionedCryptoFixture(t)
	f.svc.safetyTracker = NewAEADSafetyTracker()
	calls := &atomic.Int64{}
	cryptoMgr := f.svc.cryptoManager.(*mockCryptoManager)
	cryptoMgr.encryptFn = func(_ context.Context, _ string, plaintext, _ []byte) ([]byte, error) {
		calls.Add(1)
		next := f.version.Add(1)
		f.kmsMgr.keys[versionStatusKeyID].version = next
		return sealAtFixtureVersion(f, plaintext)
	}

	resp, err := f.svc.Encrypt(systemTenantCtx(), &EncryptRequest{KeyID: versionStatusKeyID, Plaintext: []byte("p")})
	require.Error(t, err)
	require.Nil(t, resp)
	var qbErr *errors.QBError
	require.True(t, stderrors.As(err, &qbErr), "want QBError, got %v", err)
	require.Equal(t, errors.ErrUnavailable, qbErr.Code)
	require.Equal(t, int64(sealVersionAttempts), calls.Load())
}
