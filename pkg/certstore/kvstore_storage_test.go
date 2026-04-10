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

package certstore

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errMockStorage is a sentinel error returned by the mock KVStore when
// configured to simulate failures.
var errMockStorage = errors.New("mock storage error")

// mockKVStore is an in-memory implementation of interfaces.KVStore for testing.
type mockKVStore struct {
	mu       sync.RWMutex
	data     map[string][]byte
	failPut  bool
	failGet  bool
	failDel  bool
	failList bool
}

func newMockKVStore() *mockKVStore {
	return &mockKVStore{data: make(map[string][]byte)}
}

func (m *mockKVStore) Put(_ context.Context, key string, value []byte) error {
	if m.failPut {
		return errMockStorage
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = append([]byte(nil), value...)
	return nil
}

func (m *mockKVStore) Get(_ context.Context, key string) ([]byte, error) {
	if m.failGet {
		return nil, errMockStorage
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.data[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return append([]byte(nil), v...), nil
}

func (m *mockKVStore) Delete(_ context.Context, key string) error {
	if m.failDel {
		return errMockStorage
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

func (m *mockKVStore) Scan(_ context.Context, prefix string, fn func(key string, value []byte) error) error {
	if m.failList {
		return errMockStorage
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for k, v := range m.data {
		if strings.HasPrefix(k, prefix) {
			if err := fn(k, v); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *mockKVStore) List(_ context.Context, prefix string) ([]string, error) {
	if m.failList {
		return nil, errMockStorage
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var keys []string
	for k := range m.data {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (m *mockKVStore) Exists(_ context.Context, key string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.data[key]
	return ok, nil
}

// generateTestCert creates a self-signed x509 certificate for testing.
func generateTestCert(t *testing.T, cn string) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(derBytes)
	require.NoError(t, err)
	return cert
}

// ---------------------------------------------------------------------------
// Constructor tests
// ---------------------------------------------------------------------------

func TestNewKVStoreCertStorage_Success(t *testing.T) {
	store := newMockKVStore()
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)
	assert.NotNil(t, cs)
}

func TestNewKVStoreCertStorage_NilKVStore(t *testing.T) {
	cs, err := NewKVStoreCertStorage(nil)
	assert.Nil(t, cs)
	assert.ErrorIs(t, err, ErrNilKVStore)
}

// ---------------------------------------------------------------------------
// SaveCert tests
// ---------------------------------------------------------------------------

func TestSaveCert_Success(t *testing.T) {
	ctx := context.Background()
	store := newMockKVStore()
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)

	cert := generateTestCert(t, "test-save")
	err = cs.SaveCert(ctx, "cert-1", cert)
	require.NoError(t, err)

	// Verify it was stored under the correct key.
	store.mu.RLock()
	raw, ok := store.data["certs/cert-1"]
	store.mu.RUnlock()
	assert.True(t, ok)
	assert.Equal(t, cert.Raw, raw)
}

func TestSaveCert_EmptyID(t *testing.T) {
	ctx := context.Background()
	store := newMockKVStore()
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)

	cert := generateTestCert(t, "test")
	err = cs.SaveCert(ctx, "", cert)
	assert.ErrorIs(t, err, ErrEmptyCertID)
}

func TestSaveCert_NilCert(t *testing.T) {
	ctx := context.Background()
	store := newMockKVStore()
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)

	err = cs.SaveCert(ctx, "cert-1", nil)
	assert.ErrorIs(t, err, ErrNilCertificate)
}

func TestSaveCert_StorageFailure(t *testing.T) {
	ctx := context.Background()
	store := newMockKVStore()
	store.failPut = true
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)

	cert := generateTestCert(t, "test")
	err = cs.SaveCert(ctx, "cert-1", cert)
	assert.ErrorIs(t, err, ErrStoragePut)
}

// ---------------------------------------------------------------------------
// GetCert tests
// ---------------------------------------------------------------------------

func TestGetCert_Success(t *testing.T) {
	ctx := context.Background()
	store := newMockKVStore()
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)

	cert := generateTestCert(t, "test-get")
	err = cs.SaveCert(ctx, "cert-1", cert)
	require.NoError(t, err)

	retrieved, err := cs.GetCert(ctx, "cert-1")
	require.NoError(t, err)
	assert.Equal(t, cert.Raw, retrieved.Raw)
	assert.Equal(t, cert.Subject.CommonName, retrieved.Subject.CommonName)
}

func TestGetCert_NotFound(t *testing.T) {
	ctx := context.Background()
	store := newMockKVStore()
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)

	retrieved, err := cs.GetCert(ctx, "nonexistent")
	assert.Nil(t, retrieved)
	assert.ErrorIs(t, err, ErrCertNotFound)
}

func TestGetCert_EmptyID(t *testing.T) {
	ctx := context.Background()
	store := newMockKVStore()
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)

	retrieved, err := cs.GetCert(ctx, "")
	assert.Nil(t, retrieved)
	assert.ErrorIs(t, err, ErrEmptyCertID)
}

func TestGetCert_CorruptedData(t *testing.T) {
	ctx := context.Background()
	store := newMockKVStore()
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)

	// Write garbage data directly to the store.
	store.mu.Lock()
	store.data["certs/corrupt"] = []byte("not-a-valid-der-certificate")
	store.mu.Unlock()

	retrieved, err := cs.GetCert(ctx, "corrupt")
	assert.Nil(t, retrieved)
	assert.ErrorIs(t, err, ErrCertCorrupted)
}

// ---------------------------------------------------------------------------
// DeleteCert tests
// ---------------------------------------------------------------------------

func TestDeleteCert_Success(t *testing.T) {
	ctx := context.Background()
	store := newMockKVStore()
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)

	cert := generateTestCert(t, "test-delete")
	err = cs.SaveCert(ctx, "cert-del", cert)
	require.NoError(t, err)

	err = cs.DeleteCert(ctx, "cert-del")
	require.NoError(t, err)

	// Verify it was removed.
	retrieved, err := cs.GetCert(ctx, "cert-del")
	assert.Nil(t, retrieved)
	assert.ErrorIs(t, err, ErrCertNotFound)
}

func TestDeleteCert_EmptyID(t *testing.T) {
	ctx := context.Background()
	store := newMockKVStore()
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)

	err = cs.DeleteCert(ctx, "")
	assert.ErrorIs(t, err, ErrEmptyCertID)
}

func TestDeleteCert_StorageFailure(t *testing.T) {
	ctx := context.Background()
	store := newMockKVStore()
	store.failDel = true
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)

	err = cs.DeleteCert(ctx, "cert-1")
	assert.ErrorIs(t, err, ErrStorageDelete)
}

// ---------------------------------------------------------------------------
// SaveCertChain tests
// ---------------------------------------------------------------------------

func TestSaveCertChain_Success(t *testing.T) {
	ctx := context.Background()
	store := newMockKVStore()
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)

	cert1 := generateTestCert(t, "chain-leaf")
	cert2 := generateTestCert(t, "chain-root")
	chain := []*x509.Certificate{cert1, cert2}

	err = cs.SaveCertChain(ctx, "chain-1", chain)
	require.NoError(t, err)

	store.mu.RLock()
	_, ok := store.data["certs/chain/chain-1"]
	store.mu.RUnlock()
	assert.True(t, ok)
}

func TestSaveCertChain_EmptyID(t *testing.T) {
	ctx := context.Background()
	store := newMockKVStore()
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)

	cert := generateTestCert(t, "chain-cert")
	err = cs.SaveCertChain(ctx, "", []*x509.Certificate{cert})
	assert.ErrorIs(t, err, ErrEmptyCertID)
}

func TestSaveCertChain_EmptyChain(t *testing.T) {
	ctx := context.Background()
	store := newMockKVStore()
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)

	err = cs.SaveCertChain(ctx, "chain-1", nil)
	assert.ErrorIs(t, err, ErrEmptyCertChain)

	err = cs.SaveCertChain(ctx, "chain-1", []*x509.Certificate{})
	assert.ErrorIs(t, err, ErrEmptyCertChain)
}

func TestSaveCertChain_NilCertInChain(t *testing.T) {
	ctx := context.Background()
	store := newMockKVStore()
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)

	cert := generateTestCert(t, "valid")
	err = cs.SaveCertChain(ctx, "chain-1", []*x509.Certificate{cert, nil})
	assert.ErrorIs(t, err, ErrNilCertInChain)
}

func TestSaveCertChain_StorageFailure(t *testing.T) {
	ctx := context.Background()
	store := newMockKVStore()
	store.failPut = true
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)

	cert := generateTestCert(t, "chain-cert")
	err = cs.SaveCertChain(ctx, "chain-1", []*x509.Certificate{cert})
	assert.ErrorIs(t, err, ErrStoragePut)
}

// ---------------------------------------------------------------------------
// GetCertChain tests
// ---------------------------------------------------------------------------

func TestGetCertChain_Success(t *testing.T) {
	ctx := context.Background()
	store := newMockKVStore()
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)

	cert1 := generateTestCert(t, "chain-leaf")
	cert2 := generateTestCert(t, "chain-intermediate")
	cert3 := generateTestCert(t, "chain-root")
	chain := []*x509.Certificate{cert1, cert2, cert3}

	err = cs.SaveCertChain(ctx, "chain-1", chain)
	require.NoError(t, err)

	retrieved, err := cs.GetCertChain(ctx, "chain-1")
	require.NoError(t, err)
	require.Len(t, retrieved, 3)
	assert.Equal(t, cert1.Raw, retrieved[0].Raw)
	assert.Equal(t, cert2.Raw, retrieved[1].Raw)
	assert.Equal(t, cert3.Raw, retrieved[2].Raw)
}

func TestGetCertChain_NotFound(t *testing.T) {
	ctx := context.Background()
	store := newMockKVStore()
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)

	retrieved, err := cs.GetCertChain(ctx, "nonexistent")
	assert.Nil(t, retrieved)
	assert.ErrorIs(t, err, ErrCertChainNotFound)
}

func TestGetCertChain_EmptyID(t *testing.T) {
	ctx := context.Background()
	store := newMockKVStore()
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)

	retrieved, err := cs.GetCertChain(ctx, "")
	assert.Nil(t, retrieved)
	assert.ErrorIs(t, err, ErrEmptyCertID)
}

func TestGetCertChain_CorruptedData(t *testing.T) {
	ctx := context.Background()
	store := newMockKVStore()
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)

	// Write garbage data.
	store.mu.Lock()
	store.data["certs/chain/corrupt"] = []byte("bad-chain-data")
	store.mu.Unlock()

	retrieved, err := cs.GetCertChain(ctx, "corrupt")
	assert.Nil(t, retrieved)
	assert.ErrorIs(t, err, ErrChainCorrupted)
}

func TestGetCertChain_TruncatedLengthHeader(t *testing.T) {
	ctx := context.Background()
	store := newMockKVStore()
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)

	// Write only 2 bytes (less than the 4-byte length header).
	store.mu.Lock()
	store.data["certs/chain/truncated"] = []byte{0x00, 0x01}
	store.mu.Unlock()

	retrieved, err := cs.GetCertChain(ctx, "truncated")
	assert.Nil(t, retrieved)
	assert.ErrorIs(t, err, ErrChainCorrupted)
}

func TestGetCertChain_TruncatedCertData(t *testing.T) {
	ctx := context.Background()
	store := newMockKVStore()
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)

	// Write a length header claiming 1000 bytes but provide only 2.
	data := make([]byte, 6)
	data[0] = 0x00
	data[1] = 0x00
	data[2] = 0x03
	data[3] = 0xE8 // 1000
	data[4] = 0xAA
	data[5] = 0xBB
	store.mu.Lock()
	store.data["certs/chain/short"] = data
	store.mu.Unlock()

	retrieved, err := cs.GetCertChain(ctx, "short")
	assert.Nil(t, retrieved)
	assert.ErrorIs(t, err, ErrChainCorrupted)
}

func TestGetCertChain_ZeroLengthCert(t *testing.T) {
	ctx := context.Background()
	store := newMockKVStore()
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)

	// Write a length header of zero.
	data := []byte{0x00, 0x00, 0x00, 0x00}
	store.mu.Lock()
	store.data["certs/chain/zerolen"] = data
	store.mu.Unlock()

	retrieved, err := cs.GetCertChain(ctx, "zerolen")
	assert.Nil(t, retrieved)
	assert.ErrorIs(t, err, ErrChainCorrupted)
}

// ---------------------------------------------------------------------------
// ListCerts tests
// ---------------------------------------------------------------------------

func TestListCerts_Success(t *testing.T) {
	ctx := context.Background()
	store := newMockKVStore()
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)

	cert1 := generateTestCert(t, "list-1")
	cert2 := generateTestCert(t, "list-2")
	err = cs.SaveCert(ctx, "alpha", cert1)
	require.NoError(t, err)
	err = cs.SaveCert(ctx, "beta", cert2)
	require.NoError(t, err)

	// Also save a chain to verify it is excluded.
	err = cs.SaveCertChain(ctx, "gamma", []*x509.Certificate{cert1})
	require.NoError(t, err)

	ids, err := cs.ListCerts(ctx)
	require.NoError(t, err)
	assert.Len(t, ids, 2)
	assert.Contains(t, ids, "alpha")
	assert.Contains(t, ids, "beta")
	// Verify chain is NOT included.
	for _, id := range ids {
		assert.NotContains(t, id, "chain/")
	}
}

func TestListCerts_Empty(t *testing.T) {
	ctx := context.Background()
	store := newMockKVStore()
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)

	ids, err := cs.ListCerts(ctx)
	require.NoError(t, err)
	assert.Empty(t, ids)
}

func TestListCerts_StorageFailure(t *testing.T) {
	ctx := context.Background()
	store := newMockKVStore()
	store.failList = true
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)

	ids, err := cs.ListCerts(ctx)
	assert.Nil(t, ids)
	assert.ErrorIs(t, err, ErrStorageList)
}

// ---------------------------------------------------------------------------
// CertExists tests
// ---------------------------------------------------------------------------

func TestCertExists_True(t *testing.T) {
	ctx := context.Background()
	store := newMockKVStore()
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)

	cert := generateTestCert(t, "exists-test")
	err = cs.SaveCert(ctx, "exists-1", cert)
	require.NoError(t, err)

	exists, err := cs.CertExists(ctx, "exists-1")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestCertExists_False(t *testing.T) {
	ctx := context.Background()
	store := newMockKVStore()
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)

	exists, err := cs.CertExists(ctx, "nonexistent")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestCertExists_EmptyID(t *testing.T) {
	ctx := context.Background()
	store := newMockKVStore()
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)

	exists, err := cs.CertExists(ctx, "")
	assert.False(t, exists)
	assert.ErrorIs(t, err, ErrEmptyCertID)
}

// ---------------------------------------------------------------------------
// Close tests
// ---------------------------------------------------------------------------

func TestClose_Success(t *testing.T) {
	store := newMockKVStore()
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)

	err = cs.Close()
	assert.NoError(t, err)
}

func TestClose_Idempotent(t *testing.T) {
	store := newMockKVStore()
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)

	err = cs.Close()
	assert.NoError(t, err)
	err = cs.Close()
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Encoding round-trip tests
// ---------------------------------------------------------------------------

func TestEncodeDecode_SingleCert(t *testing.T) {
	cert := generateTestCert(t, "roundtrip-single")
	chain := []*x509.Certificate{cert}

	encoded := encodeCertChain(chain)
	decoded, err := decodeCertChain(encoded)
	require.NoError(t, err)
	require.Len(t, decoded, 1)
	assert.Equal(t, cert.Raw, decoded[0].Raw)
}

func TestEncodeDecode_MultipleCerts(t *testing.T) {
	cert1 := generateTestCert(t, "roundtrip-1")
	cert2 := generateTestCert(t, "roundtrip-2")
	cert3 := generateTestCert(t, "roundtrip-3")
	chain := []*x509.Certificate{cert1, cert2, cert3}

	encoded := encodeCertChain(chain)
	decoded, err := decodeCertChain(encoded)
	require.NoError(t, err)
	require.Len(t, decoded, 3)
	for i, cert := range chain {
		assert.Equal(t, cert.Raw, decoded[i].Raw)
	}
}

func TestDecodeCertChain_EmptyData(t *testing.T) {
	decoded, err := decodeCertChain([]byte{})
	assert.Nil(t, decoded)
	assert.ErrorIs(t, err, ErrChainCorrupted)
}

// ---------------------------------------------------------------------------
// NilCertInChainError tests
// ---------------------------------------------------------------------------

func TestNilCertInChainError_ErrorMessage(t *testing.T) {
	err := &NilCertInChainError{Index: 2}
	assert.Equal(t, ErrNilCertInChain.Error(), err.Error())
}

func TestNilCertInChainError_Is(t *testing.T) {
	err := &NilCertInChainError{Index: 0}
	assert.ErrorIs(t, err, ErrNilCertInChain)
}

// ---------------------------------------------------------------------------
// SaveCert then overwrite test
// ---------------------------------------------------------------------------

func TestSaveCert_Overwrite(t *testing.T) {
	ctx := context.Background()
	store := newMockKVStore()
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)

	cert1 := generateTestCert(t, "original")
	cert2 := generateTestCert(t, "replacement")

	err = cs.SaveCert(ctx, "overwrite-1", cert1)
	require.NoError(t, err)

	err = cs.SaveCert(ctx, "overwrite-1", cert2)
	require.NoError(t, err)

	retrieved, err := cs.GetCert(ctx, "overwrite-1")
	require.NoError(t, err)
	assert.Equal(t, cert2.Raw, retrieved.Raw)
	assert.Equal(t, "replacement", retrieved.Subject.CommonName)
}

// ---------------------------------------------------------------------------
// SaveCertChain then overwrite test
// ---------------------------------------------------------------------------

func TestSaveCertChain_Overwrite(t *testing.T) {
	ctx := context.Background()
	store := newMockKVStore()
	cs, err := NewKVStoreCertStorage(store)
	require.NoError(t, err)

	cert1 := generateTestCert(t, "first-chain-cert")
	cert2 := generateTestCert(t, "second-chain-cert")
	cert3 := generateTestCert(t, "third-chain-cert")

	err = cs.SaveCertChain(ctx, "chain-overwrite", []*x509.Certificate{cert1})
	require.NoError(t, err)

	err = cs.SaveCertChain(ctx, "chain-overwrite", []*x509.Certificate{cert2, cert3})
	require.NoError(t, err)

	retrieved, err := cs.GetCertChain(ctx, "chain-overwrite")
	require.NoError(t, err)
	require.Len(t, retrieved, 2)
	assert.Equal(t, cert2.Raw, retrieved[0].Raw)
	assert.Equal(t, cert3.Raw, retrieved[1].Raw)
}
