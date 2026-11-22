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

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
)

// versionedCryptoFixture is a crypto service mounted over a KMS service whose
// crypto manager seals ciphertexts the way XKMSBackend does: JSON
// EncryptedData carrying the sealing key version in the "kv" metadata entry.
type versionedCryptoFixture struct {
	svc     *cryptoService
	kmsSvc  *kmsServiceImpl
	kmsMgr  *mockKMSManager
	scoped  string
	version *atomic.Uint64
	decrypt *atomic.Int64
}

const versionStatusKeyID = "version-status-key"

func newVersionedCryptoFixture(t *testing.T) *versionedCryptoFixture {
	t.Helper()
	ctx := systemTenantCtx()

	version := &atomic.Uint64{}
	version.Store(1)
	decrypts := &atomic.Int64{}

	kmsMgr := newMockKMSManager()
	kmsMgr.keys[versionStatusKeyID] = &mockKeyData{
		keyID:     versionStatusKeyID,
		algorithm: "AES-256-GCM",
		version:   1,
		status:    "enabled",
		createdAt: time.Now(),
	}

	open := func(ciphertext []byte) ([]byte, error) {
		var encrypted kctypes.EncryptedData
		if err := json.Unmarshal(ciphertext, &encrypted); err != nil {
			return nil, err
		}
		decrypts.Add(1)
		return encrypted.Ciphertext, nil
	}
	cryptoMgr := newMockCryptoManager()
	cryptoMgr.encryptFn = func(_ context.Context, _ string, plaintext, _ []byte) ([]byte, error) {
		return json.Marshal(kctypes.EncryptedData{
			Ciphertext: plaintext,
			Metadata:   map[string][]byte{"kv": []byte(fmt.Sprintf("%d", version.Load()))},
		})
	}
	cryptoMgr.decryptFn = func(_ context.Context, _ string, ciphertext, _ []byte) ([]byte, error) {
		return open(ciphertext)
	}
	cryptoMgr.decryptBatchFn = func(_ context.Context, _ string, items [][]byte, _ []byte) ([][]byte, error) {
		out := make([][]byte, len(items))
		for i, item := range items {
			pt, err := open(item)
			if err != nil {
				return nil, err
			}
			out[i] = pt
		}
		return out, nil
	}

	logger := newMockLogger()
	kmsSvc := newKMSService(kmsMgr, logger)
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)
	svc.kmsSvc = kmsSvc

	scoped, err := scopeKeyID(ctx, versionStatusKeyID)
	require.NoError(t, err)

	return &versionedCryptoFixture{svc: svc, kmsSvc: kmsSvc, kmsMgr: kmsMgr, scoped: scoped, version: version, decrypt: decrypts}
}

func (f *versionedCryptoFixture) encrypt(t *testing.T, plaintext string) []byte {
	t.Helper()
	resp, err := f.svc.Encrypt(systemTenantCtx(), &EncryptRequest{KeyID: versionStatusKeyID, Plaintext: []byte(plaintext)})
	require.NoError(t, err)
	return resp.Ciphertext
}

// rotateAndDisableV1 moves the key to version 2 and marks version 1 disabled,
// the state v1's grace-period job leaves behind.
func (f *versionedCryptoFixture) rotateAndDisableV1() {
	f.version.Store(2)
	// The key's current version moves to 2 as well: encryption checks it.
	f.kmsMgr.keys[versionStatusKeyID].version = 2
	now := time.Now()
	f.kmsSvc.setVersionStatus(f.scoped, 1, KeyStatusDisabled, &now)
}

func requireKeyDisabled(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var disabled *api.KeyDisabledError
	require.True(t, stderrors.As(err, &disabled), "want KeyDisabledError, got %v", err)
}

func TestCryptoService_Decrypt_DisabledKeyVersionIsRefused(t *testing.T) {
	f := newVersionedCryptoFixture(t)
	v1 := f.encrypt(t, "sealed under v1")

	resp, err := f.svc.Decrypt(systemTenantCtx(), &DecryptRequest{KeyID: versionStatusKeyID, Ciphertext: v1})
	require.NoError(t, err)
	require.Equal(t, "sealed under v1", string(resp.Plaintext))

	f.rotateAndDisableV1()
	before := f.decrypt.Load()

	_, err = f.svc.Decrypt(systemTenantCtx(), &DecryptRequest{KeyID: versionStatusKeyID, Ciphertext: v1})
	requireKeyDisabled(t, err)
	require.Equal(t, before, f.decrypt.Load(), "a disabled version never reaches the crypto manager")
}

func TestCryptoService_DecryptBatch_DisabledKeyVersionFailsTheBatch(t *testing.T) {
	f := newVersionedCryptoFixture(t)
	v1 := f.encrypt(t, "old")
	f.rotateAndDisableV1()
	v2a := f.encrypt(t, "new-a")
	v2b := f.encrypt(t, "new-b")
	before := f.decrypt.Load()

	_, err := f.svc.DecryptBatch(systemTenantCtx(), &DecryptBatchRequest{
		KeyID: versionStatusKeyID,
		Items: []BatchDecryptItem{{Ciphertext: v2a}, {Ciphertext: v1}, {Ciphertext: v2b}},
	})
	requireKeyDisabled(t, err)
	require.Equal(t, before, f.decrypt.Load(), "no item of a refused batch is decrypted")
}

func TestCryptoService_Decrypt_EnabledKeyVersionStillDecrypts(t *testing.T) {
	f := newVersionedCryptoFixture(t)
	f.rotateAndDisableV1()
	v2 := f.encrypt(t, "sealed under v2")

	resp, err := f.svc.Decrypt(systemTenantCtx(), &DecryptRequest{KeyID: versionStatusKeyID, Ciphertext: v2})
	require.NoError(t, err)
	require.Equal(t, "sealed under v2", string(resp.Plaintext))

	batch, err := f.svc.DecryptBatch(systemTenantCtx(), &DecryptBatchRequest{
		KeyID: versionStatusKeyID,
		Items: []BatchDecryptItem{{Ciphertext: v2}, {Ciphertext: v2}},
	})
	require.NoError(t, err)
	require.Len(t, batch.Results, 2)
	require.Equal(t, "sealed under v2", string(batch.Results[1].Plaintext))
}

func TestCryptoService_Decrypt_UnparsableCiphertextGoesToBackend(t *testing.T) {
	f := newVersionedCryptoFixture(t)
	f.rotateAndDisableV1()
	opaque := []byte("ct")

	require.NoError(t, f.svc.checkVersionUsable(systemTenantCtx(), f.scoped, opaque))

	_, err := f.svc.Decrypt(systemTenantCtx(), &DecryptRequest{KeyID: versionStatusKeyID, Ciphertext: opaque})
	require.Error(t, err, "the crypto backend rejects a ciphertext it cannot decode")
	var disabled *api.KeyDisabledError
	require.False(t, stderrors.As(err, &disabled), "an unparsable ciphertext is not refused as key-disabled: %v", err)
}

func TestCryptoService_Decrypt_NonpositiveKeyVersionRefused(t *testing.T) {
	f := newVersionedCryptoFixture(t)
	blob, err := json.Marshal(kctypes.EncryptedData{
		Ciphertext: []byte("sealed"),
		Metadata:   map[string][]byte{"kv": []byte("-1")},
	})
	require.NoError(t, err)
	before := f.decrypt.Load()

	requireInvalidInput := func(err error) {
		t.Helper()
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, stderrors.As(err, &qbErr), "want QBError, got %v", err)
		require.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	}

	_, err = f.svc.Decrypt(systemTenantCtx(), &DecryptRequest{KeyID: versionStatusKeyID, Ciphertext: blob})
	requireInvalidInput(err)

	_, err = f.svc.DecryptBatch(systemTenantCtx(), &DecryptBatchRequest{
		KeyID: versionStatusKeyID,
		Items: []BatchDecryptItem{{Ciphertext: blob}},
	})
	requireInvalidInput(err)

	require.Equal(t, before, f.decrypt.Load(), "a ciphertext with an invalid key version decrypts nothing")
}

// failingGetKVStore is a KVStore whose Get fails with an error that is not not-found.
type failingGetKVStore struct {
	*mockKVStoreForKMS
	getErr error
}

func (s *failingGetKVStore) Get(context.Context, string) ([]byte, error) {
	return nil, s.getErr
}

// failingQuerierScheduler is a scheduler whose completed grace-period query fails.
type failingQuerierScheduler struct {
	queryErr error
}

func (s *failingQuerierScheduler) ScheduleJob(context.Context, *SchedulerJob) error { return nil }

func (s *failingQuerierScheduler) GetCompletedGracePeriodVersions(context.Context, string) (map[uint64]time.Time, error) {
	return nil, s.queryErr
}

// requireStatusUnavailable asserts both Decrypt and DecryptBatch of ciphertext fail, and the crypto manager decrypts nothing.
func requireStatusUnavailable(t *testing.T, f *versionedCryptoFixture, ciphertext []byte) {
	t.Helper()
	before := f.decrypt.Load()

	_, err := f.svc.Decrypt(systemTenantCtx(), &DecryptRequest{KeyID: versionStatusKeyID, Ciphertext: ciphertext})
	require.Error(t, err)
	require.Contains(t, err.Error(), "service.Decrypt: unavailable")

	_, err = f.svc.DecryptBatch(systemTenantCtx(), &DecryptBatchRequest{
		KeyID: versionStatusKeyID,
		Items: []BatchDecryptItem{{Ciphertext: ciphertext}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "service.Decrypt: unavailable")

	require.Equal(t, before, f.decrypt.Load(), "a version whose status cannot be read never reaches the crypto manager")
}

func TestCryptoService_Decrypt_VersionStatusReadErrorFailsClosed(t *testing.T) {
	f := newVersionedCryptoFixture(t)
	v1 := f.encrypt(t, "sealed under v1")
	f.kmsSvc.SetKVStore(&failingGetKVStore{
		mockKVStoreForKMS: &mockKVStoreForKMS{data: make(map[string][]byte)},
		getErr:            stderrors.New("raft: no leader"),
	})

	requireStatusUnavailable(t, f, v1)
}

type failingPutKVStore struct {
	*mockKVStoreForKMS
	putErr error
}

func (s *failingPutKVStore) Put(ctx context.Context, key string, value []byte) error {
	if s.putErr != nil {
		return s.putErr
	}
	return s.mockKVStoreForKMS.Put(ctx, key, value)
}

func newFailingPutKVStore(t *testing.T, scoped string) *failingPutKVStore {
	t.Helper()
	enabled, err := json.Marshal(&persistedVersionStatus{Status: string(KeyStatusEnabled)})
	require.NoError(t, err)
	return &failingPutKVStore{
		mockKVStoreForKMS: &mockKVStoreForKMS{data: map[string][]byte{versionStatusKey(scoped, 1): enabled}},
		putErr:            stderrors.New("raft: no leader"),
	}
}

func requireV1Refused(t *testing.T, f *versionedCryptoFixture, v1 []byte) {
	t.Helper()
	before := f.decrypt.Load()
	_, err := f.svc.Decrypt(systemTenantCtx(), &DecryptRequest{KeyID: versionStatusKeyID, Ciphertext: v1})
	requireKeyDisabled(t, err)
	_, err = f.svc.DecryptBatch(systemTenantCtx(), &DecryptBatchRequest{
		KeyID: versionStatusKeyID,
		Items: []BatchDecryptItem{{Ciphertext: v1}},
	})
	requireKeyDisabled(t, err)
	require.Equal(t, before, f.decrypt.Load(), "a version whose disable is pending never reaches the crypto manager")
}

func TestKMSUpdateKeyStatus_FailedDisableWriteFailsClosed(t *testing.T) {
	f := newVersionedCryptoFixture(t)
	store := newFailingPutKVStore(t, f.scoped)
	f.kmsSvc.SetKVStore(store)
	v1 := f.encrypt(t, "sealed under v1")

	req := &UpdateKeyStatusRequest{KeyID: versionStatusKeyID, Version: 1, Status: KeyStatusDisabled}
	_, err := f.kmsSvc.UpdateKeyStatus(systemTenantCtx(), req)
	require.Error(t, err, "a disable that is not persisted is reported to the caller")
	var qbErr *errors.QBError
	require.True(t, stderrors.As(err, &qbErr), "want QBError, got %v", err)
	require.Equal(t, errors.ErrStorage, qbErr.Code)
	requireV1Refused(t, f, v1)

	store.putErr = nil
	_, err = f.kmsSvc.UpdateKeyStatus(systemTenantCtx(), req)
	require.NoError(t, err, "a retry persists the pending disable")
	data, err := store.Get(systemTenantCtx(), versionStatusKey(f.scoped, 1))
	require.NoError(t, err)
	var ps persistedVersionStatus
	require.NoError(t, json.Unmarshal(data, &ps))
	require.Equal(t, string(KeyStatusDisabled), ps.Status)
	requireV1Refused(t, f, v1)

	t.Run("grace period disable", func(t *testing.T) {
		g := newVersionedCryptoFixture(t)
		g.kmsSvc.SetKVStore(newFailingPutKVStore(t, g.scoped))
		gv1 := g.encrypt(t, "sealed under v1")

		_, err := g.kmsSvc.disableKeyVersion(systemTenantCtx(), g.scoped, 1)
		require.Error(t, err)
		before := g.decrypt.Load()
		_, err = g.svc.Decrypt(systemTenantCtx(), &DecryptRequest{KeyID: versionStatusKeyID, Ciphertext: gv1})
		requireKeyDisabled(t, err)
		require.Equal(t, before, g.decrypt.Load())
	})
}

func TestCryptoService_Decrypt_VersionStatusUndecodableFailsClosed(t *testing.T) {
	f := newVersionedCryptoFixture(t)
	v1 := f.encrypt(t, "sealed under v1")
	f.kmsSvc.SetKVStore(&mockKVStoreForKMS{data: map[string][]byte{
		versionStatusKey(f.scoped, 1): []byte("not-json"),
	}})

	requireStatusUnavailable(t, f, v1)
}

func TestCryptoService_Decrypt_GracePeriodQueryErrorFailsClosed(t *testing.T) {
	f := newVersionedCryptoFixture(t)
	v1 := f.encrypt(t, "sealed under v1")
	f.kmsSvc.SetScheduler(&failingQuerierScheduler{queryErr: stderrors.New("scheduler: sync read failed")})

	requireStatusUnavailable(t, f, v1)
}

func TestCryptoService_Decrypt_AbsentVersionStatusStillDecrypts(t *testing.T) {
	f := newVersionedCryptoFixture(t)
	v1 := f.encrypt(t, "sealed under v1")
	f.kmsSvc.SetKVStore(&mockKVStoreForKMS{data: make(map[string][]byte)})

	resp, err := f.svc.Decrypt(systemTenantCtx(), &DecryptRequest{KeyID: versionStatusKeyID, Ciphertext: v1})
	require.NoError(t, err)
	require.Equal(t, "sealed under v1", string(resp.Plaintext))

	batch, err := f.svc.DecryptBatch(systemTenantCtx(), &DecryptBatchRequest{
		KeyID: versionStatusKeyID,
		Items: []BatchDecryptItem{{Ciphertext: v1}},
	})
	require.NoError(t, err)
	require.NotNil(t, batch)
}

// TestCryptoService_Decrypt_VersionDisabledOnAnotherNodeIsRefused: node A
// caches v1 as enabled, node B disables v1 in the shared replicated store,
// and A's decrypt gate honours B's write instead of its own cache.
func TestCryptoService_Decrypt_VersionDisabledOnAnotherNodeIsRefused(t *testing.T) {
	f := newVersionedCryptoFixture(t)
	shared := &mockKVStoreForKMS{data: map[string][]byte{}}
	f.kmsSvc.SetKVStore(shared)
	nodeB := newKMSService(newMockKMSManager(), newMockLogger())
	nodeB.SetKVStore(shared)

	nodeB.setVersionStatus(f.scoped, 1, KeyStatusEnabled, nil)

	v1 := f.encrypt(t, "sealed under v1")
	_, err := f.svc.Decrypt(systemTenantCtx(), &DecryptRequest{KeyID: versionStatusKeyID, Ciphertext: v1})
	require.NoError(t, err, "v1 decrypts while enabled, leaving enabled in node A's cache")

	_, err = nodeB.disableKeyVersion(systemTenantCtx(), f.scoped, 1)
	require.NoError(t, err)

	before := f.decrypt.Load()
	_, err = f.svc.Decrypt(systemTenantCtx(), &DecryptRequest{KeyID: versionStatusKeyID, Ciphertext: v1})
	requireKeyDisabled(t, err)
	_, err = f.svc.DecryptBatch(systemTenantCtx(), &DecryptBatchRequest{
		KeyID: versionStatusKeyID,
		Items: []BatchDecryptItem{{Ciphertext: v1}},
	})
	requireKeyDisabled(t, err)
	require.Equal(t, before, f.decrypt.Load(), "a version disabled on another node releases no plaintext")

	t.Run("re-enabled on another node", func(t *testing.T) {
		nodeB.setVersionStatus(f.scoped, 1, KeyStatusEnabled, nil)
		resp, err := f.svc.Decrypt(systemTenantCtx(), &DecryptRequest{KeyID: versionStatusKeyID, Ciphertext: v1})
		require.NoError(t, err)
		require.Equal(t, "sealed under v1", string(resp.Plaintext))
	})
}

// TestCryptoService_Decrypt_ReportsCiphertextKeyVersion: Decrypt reports the
// version the ciphertext was sealed under, and refuses an explicit version
// that conflicts with it before anything is decrypted.
func TestCryptoService_Decrypt_ReportsCiphertextKeyVersion(t *testing.T) {
	f := newVersionedCryptoFixture(t)
	f.version.Store(2)
	v2 := f.encrypt(t, "sealed under v2")

	resp, err := f.svc.Decrypt(systemTenantCtx(), &DecryptRequest{KeyID: versionStatusKeyID, Ciphertext: v2})
	require.NoError(t, err)
	require.Equal(t, uint64(2), resp.KeyVersion)

	resp, err = f.svc.Decrypt(systemTenantCtx(), &DecryptRequest{KeyID: versionStatusKeyID, Ciphertext: v2, KeyVersion: 2})
	require.NoError(t, err)
	require.Equal(t, uint64(2), resp.KeyVersion)

	before := f.decrypt.Load()
	_, err = f.svc.Decrypt(systemTenantCtx(), &DecryptRequest{KeyID: versionStatusKeyID, Ciphertext: v2, KeyVersion: 1})
	require.Error(t, err)
	var qbErr *errors.QBError
	require.True(t, stderrors.As(err, &qbErr), "want QBError, got %v", err)
	require.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	require.Equal(t, before, f.decrypt.Load(), "a conflicting version decrypts nothing")
}

// TestCryptoService_Encrypt_ReportsCiphertextVersionAcrossRotation: another
// node rotates the key from v1 to v2 after Encrypt produced its v1 ciphertext
// and before the response is built. The response reports v1, the version the
// ciphertext records, and decrypting with that reported version succeeds.
func TestCryptoService_Encrypt_ReportsCiphertextVersionAcrossRotation(t *testing.T) {
	f := newVersionedCryptoFixture(t)
	current := &atomic.Int64{}
	current.Store(1)
	f.kmsMgr.getKeyFn = func(_ context.Context, keyID string) (interface{}, error) {
		return &kms.KeyMetadata{KeyID: keyID, Algorithm: "AES-256-GCM", Version: int(current.Load()), State: kms.KeyStateEnabled}, nil
	}
	cryptoMgr := f.svc.cryptoManager.(*mockCryptoManager)
	seal := func(plaintext []byte) ([]byte, error) {
		return json.Marshal(kctypes.EncryptedData{
			Ciphertext: plaintext,
			Metadata:   map[string][]byte{"kv": []byte("1")},
		})
	}
	cryptoMgr.encryptFn = func(_ context.Context, _ string, plaintext, _ []byte) ([]byte, error) {
		ct, err := seal(plaintext)
		current.Store(2) // the rotation lands once the v1 ciphertext exists
		return ct, err
	}
	cryptoMgr.encryptBatchFn = func(_ context.Context, _ string, items [][]byte, _ []byte) ([][]byte, error) {
		out := make([][]byte, len(items))
		for i, item := range items {
			ct, err := seal(item)
			if err != nil {
				return nil, err
			}
			out[i] = ct
		}
		current.Store(2)
		return out, nil
	}
	ctx := systemTenantCtx()

	resp, err := f.svc.Encrypt(ctx, &EncryptRequest{KeyID: versionStatusKeyID, Plaintext: []byte("sealed under v1")})
	require.NoError(t, err)
	require.Equal(t, uint64(1), resp.KeyVersion, "the response names the version the ciphertext records")
	_, err = f.svc.Decrypt(ctx, &DecryptRequest{KeyID: versionStatusKeyID, Ciphertext: resp.Ciphertext, KeyVersion: resp.KeyVersion})
	require.NoError(t, err, "the reported version decrypts its own ciphertext")

	current.Store(1)
	batch, err := f.svc.EncryptBatch(ctx, &EncryptBatchRequest{KeyID: versionStatusKeyID, Items: []BatchEncryptItem{{Plaintext: []byte("a")}}})
	require.NoError(t, err)
	require.Equal(t, uint64(1), batch.KeyVersion, "the batch response names the version its ciphertexts record")
}
