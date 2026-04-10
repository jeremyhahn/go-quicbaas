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
	"crypto/x509"
	"encoding/binary"
	"strings"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// Compile-time interface compliance check.
var _ xkmssdk.CertificateStorageAdapter = (*KVStoreCertStorage)(nil)

// kvStoreTimeout is the default timeout for KVStore operations.
// QuicRaft's SyncRead requires a context with a deadline.
const kvStoreTimeout = 30 * time.Second

// certPrefix is the key prefix for individual certificates.
const certPrefix = "certs/"

// chainPrefix is the key prefix for certificate chains.
const chainPrefix = "certs/chain/"

// KVStoreCertStorage adapts QuicBaaS's KVStore interface to go-xkms's
// CertificateStorage interface. Certificates are stored as DER-encoded
// byte slices. Certificate chains are stored as length-prefixed sequences
// of DER-encoded certificates using big-endian uint32 length headers.
//
// Key layout:
//   - Individual certs: certs/{id}
//   - Certificate chains: certs/chain/{id}
type KVStoreCertStorage struct {
	kvstore interfaces.KVStore
}

// NewKVStoreCertStorage creates a new KVStoreCertStorage backed by the
// provided KVStore. Returns ErrNilKVStore if kvstore is nil.
func NewKVStoreCertStorage(kvstore interfaces.KVStore) (*KVStoreCertStorage, error) {
	if kvstore == nil {
		return nil, ErrNilKVStore
	}
	return &KVStoreCertStorage{kvstore: kvstore}, nil
}

// SaveCert stores a DER-encoded certificate under certs/{id}.
func (s *KVStoreCertStorage) SaveCert(ctx context.Context, id string, cert *x509.Certificate) error {
	if id == "" {
		return ErrEmptyCertID
	}
	if cert == nil {
		return ErrNilCertificate
	}
	kvCtx, cancel := context.WithTimeout(ctx, kvStoreTimeout)
	defer cancel()
	key := certPrefix + id
	err := s.kvstore.Put(kvCtx, key, cert.Raw)
	if err != nil {
		return ErrStoragePut
	}
	return nil
}

// GetCert retrieves a certificate by ID and parses it from DER encoding.
func (s *KVStoreCertStorage) GetCert(ctx context.Context, id string) (*x509.Certificate, error) {
	if id == "" {
		return nil, ErrEmptyCertID
	}
	kvCtx, cancel := context.WithTimeout(ctx, kvStoreTimeout)
	defer cancel()
	key := certPrefix + id
	data, err := s.kvstore.Get(kvCtx, key)
	if err != nil {
		return nil, ErrCertNotFound
	}
	cert, err := x509.ParseCertificate(data)
	if err != nil {
		return nil, ErrCertCorrupted
	}
	return cert, nil
}

// DeleteCert removes a certificate by ID from storage.
func (s *KVStoreCertStorage) DeleteCert(ctx context.Context, id string) error {
	if id == "" {
		return ErrEmptyCertID
	}
	kvCtx, cancel := context.WithTimeout(ctx, kvStoreTimeout)
	defer cancel()
	key := certPrefix + id
	err := s.kvstore.Delete(kvCtx, key)
	if err != nil {
		return ErrStorageDelete
	}
	return nil
}

// SaveCertChain stores a certificate chain as length-prefixed DER blobs.
// Each certificate is preceded by a 4-byte big-endian uint32 length header.
func (s *KVStoreCertStorage) SaveCertChain(ctx context.Context, id string, chain []*x509.Certificate) error {
	if id == "" {
		return ErrEmptyCertID
	}
	if len(chain) == 0 {
		return ErrEmptyCertChain
	}
	for i, cert := range chain {
		if cert == nil {
			return &NilCertInChainError{Index: i}
		}
	}
	encoded := encodeCertChain(chain)
	kvCtx, cancel := context.WithTimeout(ctx, kvStoreTimeout)
	defer cancel()
	key := chainPrefix + id
	err := s.kvstore.Put(kvCtx, key, encoded)
	if err != nil {
		return ErrStoragePut
	}
	return nil
}

// GetCertChain retrieves and decodes a certificate chain by ID.
func (s *KVStoreCertStorage) GetCertChain(ctx context.Context, id string) ([]*x509.Certificate, error) {
	if id == "" {
		return nil, ErrEmptyCertID
	}
	kvCtx, cancel := context.WithTimeout(ctx, kvStoreTimeout)
	defer cancel()
	key := chainPrefix + id
	data, err := s.kvstore.Get(kvCtx, key)
	if err != nil {
		return nil, ErrCertChainNotFound
	}
	chain, err := decodeCertChain(data)
	if err != nil {
		return nil, ErrChainCorrupted
	}
	return chain, nil
}

// ListCerts returns the IDs of all stored individual certificates.
// Chain keys (under certs/chain/) are excluded.
func (s *KVStoreCertStorage) ListCerts(ctx context.Context) ([]string, error) {
	kvCtx, cancel := context.WithTimeout(ctx, kvStoreTimeout)
	defer cancel()
	keys, err := s.kvstore.List(kvCtx, certPrefix)
	if err != nil {
		return nil, ErrStorageList
	}
	var ids []string
	for _, key := range keys {
		// Skip chain keys; they live under certs/chain/.
		if strings.HasPrefix(key, chainPrefix) {
			continue
		}
		id := strings.TrimPrefix(key, certPrefix)
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// CertExists checks whether a certificate with the given ID exists.
func (s *KVStoreCertStorage) CertExists(ctx context.Context, id string) (bool, error) {
	if id == "" {
		return false, ErrEmptyCertID
	}
	kvCtx, cancel := context.WithTimeout(ctx, kvStoreTimeout)
	defer cancel()
	key := certPrefix + id
	_, err := s.kvstore.Get(kvCtx, key)
	if err != nil {
		return false, nil
	}
	return true, nil
}

// Close is a no-op because the KVStore lifecycle is managed by QuicBaaS.
func (s *KVStoreCertStorage) Close() error {
	return nil
}

// encodeCertChain encodes a certificate chain as a sequence of
// length-prefixed DER blobs. Each certificate is preceded by a 4-byte
// big-endian uint32 containing the length of its DER encoding.
func encodeCertChain(chain []*x509.Certificate) []byte {
	// Pre-calculate total size to avoid re-allocation.
	totalSize := 0
	for _, cert := range chain {
		totalSize += 4 + len(cert.Raw)
	}
	buf := make([]byte, 0, totalSize)
	lenBuf := make([]byte, 4)
	for _, cert := range chain {
		certLen := len(cert.Raw)
		if certLen > (1<<32 - 1) {
			return nil
		}
		binary.BigEndian.PutUint32(lenBuf, uint32(certLen)) //#nosec G115 -- bounded above
		buf = append(buf, lenBuf...)
		buf = append(buf, cert.Raw...)
	}
	return buf
}

// decodeCertChain decodes a length-prefixed DER blob sequence back into
// a slice of x509.Certificate pointers.
func decodeCertChain(data []byte) ([]*x509.Certificate, error) {
	var chain []*x509.Certificate
	offset := 0
	for offset < len(data) {
		if offset+4 > len(data) {
			return nil, ErrChainCorrupted
		}
		certLen := int(binary.BigEndian.Uint32(data[offset : offset+4]))
		offset += 4
		if certLen <= 0 || offset+certLen > len(data) {
			return nil, ErrChainCorrupted
		}
		cert, err := x509.ParseCertificate(data[offset : offset+certLen])
		if err != nil {
			return nil, ErrChainCorrupted
		}
		chain = append(chain, cert)
		offset += certLen
	}
	if len(chain) == 0 {
		return nil, ErrChainCorrupted
	}
	return chain, nil
}

// NilCertInChainError provides the index of the nil certificate in the chain.
type NilCertInChainError struct {
	Index int
}

// Error implements the error interface.
func (e *NilCertInChainError) Error() string {
	return ErrNilCertInChain.Error()
}

// Is allows errors.Is to match against ErrNilCertInChain.
func (e *NilCertInChainError) Is(target error) bool {
	return target == ErrNilCertInChain
}
