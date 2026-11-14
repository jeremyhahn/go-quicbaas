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

func TestSaveCert_Errors(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		cert    *x509.Certificate
		failPut bool
		wantErr error
	}{
		{name: "empty id", id: "", cert: generateTestCert(t, "test"), wantErr: ErrEmptyCertID},
		{name: "nil cert", id: "cert-1", cert: nil, wantErr: ErrNilCertificate},
		{name: "storage failure", id: "cert-1", cert: generateTestCert(t, "test"), failPut: true, wantErr: ErrStoragePut},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newMockKVStore()
			store.failPut = tt.failPut
			cs, err := NewKVStoreCertStorage(store)
			require.NoError(t, err)

			err = cs.SaveCert(context.Background(), tt.id, tt.cert)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
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

func TestGetCert_Errors(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		stored  map[string][]byte
		wantErr error
	}{
		{name: "not found", id: "nonexistent", wantErr: ErrCertNotFound},
		{name: "empty id", id: "", wantErr: ErrEmptyCertID},
		{
			name:    "corrupted data",
			id:      "corrupt",
			stored:  map[string][]byte{"certs/corrupt": []byte("not-a-valid-der-certificate")},
			wantErr: ErrCertCorrupted,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newMockKVStore()
			for k, v := range tt.stored {
				store.data[k] = v
			}
			cs, err := NewKVStoreCertStorage(store)
			require.NoError(t, err)

			retrieved, err := cs.GetCert(context.Background(), tt.id)
			assert.Nil(t, retrieved)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
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

func TestDeleteCert_Errors(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		failDel bool
		wantErr error
	}{
		{name: "empty id", id: "", wantErr: ErrEmptyCertID},
		{name: "storage failure", id: "cert-1", failDel: true, wantErr: ErrStorageDelete},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newMockKVStore()
			store.failDel = tt.failDel
			cs, err := NewKVStoreCertStorage(store)
			require.NoError(t, err)

			err = cs.DeleteCert(context.Background(), tt.id)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
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

func TestSaveCertChain_Errors(t *testing.T) {
	cert := generateTestCert(t, "chain-cert")
	tests := []struct {
		name    string
		id      string
		chain   []*x509.Certificate
		failPut bool
		wantErr error
	}{
		{name: "empty id", id: "", chain: []*x509.Certificate{cert}, wantErr: ErrEmptyCertID},
		{name: "nil chain", id: "chain-1", chain: nil, wantErr: ErrEmptyCertChain},
		{name: "empty chain", id: "chain-1", chain: []*x509.Certificate{}, wantErr: ErrEmptyCertChain},
		{name: "nil cert in chain", id: "chain-1", chain: []*x509.Certificate{cert, nil}, wantErr: ErrNilCertInChain},
		{name: "storage failure", id: "chain-1", chain: []*x509.Certificate{cert}, failPut: true, wantErr: ErrStoragePut},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newMockKVStore()
			store.failPut = tt.failPut
			cs, err := NewKVStoreCertStorage(store)
			require.NoError(t, err)

			err = cs.SaveCertChain(context.Background(), tt.id, tt.chain)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
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

func TestGetCertChain_Errors(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		stored  []byte
		wantErr error
	}{
		{name: "not found", id: "nonexistent", wantErr: ErrCertChainNotFound},
		{name: "empty id", id: "", wantErr: ErrEmptyCertID},
		{name: "corrupted data", id: "corrupt", stored: []byte("bad-chain-data"), wantErr: ErrChainCorrupted},
		// Only 2 bytes, less than the 4-byte length header.
		{name: "truncated length header", id: "truncated", stored: []byte{0x00, 0x01}, wantErr: ErrChainCorrupted},
		// A length header claiming 1000 bytes followed by only 2.
		{name: "truncated cert data", id: "short", stored: []byte{0x00, 0x00, 0x03, 0xE8, 0xAA, 0xBB}, wantErr: ErrChainCorrupted},
		{name: "zero length cert", id: "zerolen", stored: []byte{0x00, 0x00, 0x00, 0x00}, wantErr: ErrChainCorrupted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newMockKVStore()
			if tt.stored != nil {
				store.data["certs/chain/"+tt.id] = tt.stored
			}
			cs, err := NewKVStoreCertStorage(store)
			require.NoError(t, err)

			retrieved, err := cs.GetCertChain(context.Background(), tt.id)
			assert.Nil(t, retrieved)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
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

func TestCertExists(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		wantExists bool
		wantErr    error
	}{
		{name: "exists", id: "exists-1", wantExists: true},
		{name: "missing", id: "nonexistent", wantExists: false},
		{name: "empty id", id: "", wantExists: false, wantErr: ErrEmptyCertID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			cs, err := NewKVStoreCertStorage(newMockKVStore())
			require.NoError(t, err)
			require.NoError(t, cs.SaveCert(ctx, "exists-1", generateTestCert(t, "exists-test")))

			exists, err := cs.CertExists(ctx, tt.id)
			assert.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, tt.wantExists, exists)
		})
	}
}

// ---------------------------------------------------------------------------
// Close tests
// ---------------------------------------------------------------------------

func TestClose_Idempotent(t *testing.T) {
	cs, err := NewKVStoreCertStorage(newMockKVStore())
	require.NoError(t, err)

	assert.NoError(t, cs.Close())
	assert.NoError(t, cs.Close())
}

// ---------------------------------------------------------------------------
// Encoding round-trip tests
// ---------------------------------------------------------------------------

func TestEncodeDecode_RoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		chain []*x509.Certificate
	}{
		{name: "single cert", chain: []*x509.Certificate{generateTestCert(t, "roundtrip-single")}},
		{
			name: "multiple certs",
			chain: []*x509.Certificate{
				generateTestCert(t, "roundtrip-1"),
				generateTestCert(t, "roundtrip-2"),
				generateTestCert(t, "roundtrip-3"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decoded, err := decodeCertChain(encodeCertChain(tt.chain))
			require.NoError(t, err)
			require.Len(t, decoded, len(tt.chain))
			for i, cert := range tt.chain {
				assert.Equal(t, cert.Raw, decoded[i].Raw)
			}
		})
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

func TestNilCertInChainError(t *testing.T) {
	err := &NilCertInChainError{Index: 2}
	assert.Equal(t, ErrNilCertInChain.Error(), err.Error())
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
