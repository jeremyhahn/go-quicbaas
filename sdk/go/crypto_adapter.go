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

package quicbaas

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"io"

	sdkcrypto "github.com/jeremyhahn/go-quicbaas/sdk/go/client/crypto"
)

// Compile-time interface assertions.
var (
	_ crypto.Signer    = (*QuicBaaSSigner)(nil)
	_ crypto.Decrypter = (*QuicBaaSDecrypter)(nil)
)

// hashToAlgorithm maps crypto.Hash values to the SDK's HashAlgorithm type
// using map-based dispatch for O(1) lookup.
var hashToAlgorithm = map[crypto.Hash]sdkcrypto.HashAlgorithm{
	crypto.SHA256: sdkcrypto.HashSHA256,
	crypto.SHA384: sdkcrypto.HashSHA384,
	crypto.SHA512: sdkcrypto.HashSHA512,
}

// ErrSignerKeyFetch indicates a failure to fetch the public key for a signer.
type ErrSignerKeyFetch struct {
	KeyID string
	Err   error
}

// Error implements the error interface.
func (e *ErrSignerKeyFetch) Error() string {
	return "quicbaas signer: failed to fetch public key for key " + e.KeyID + ": " + e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *ErrSignerKeyFetch) Unwrap() error {
	return e.Err
}

// ErrSignerSign indicates a failure during a signing operation.
type ErrSignerSign struct {
	KeyID string
	Err   error
}

// Error implements the error interface.
func (e *ErrSignerSign) Error() string {
	return "quicbaas signer: signing failed for key " + e.KeyID + ": " + e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *ErrSignerSign) Unwrap() error {
	return e.Err
}

// ErrSignerUnsupportedHash indicates that the requested hash algorithm is not supported.
type ErrSignerUnsupportedHash struct {
	Hash crypto.Hash
}

// Error implements the error interface.
func (e *ErrSignerUnsupportedHash) Error() string {
	return "quicbaas signer: unsupported hash algorithm: " + e.Hash.String()
}

// ErrDecrypterKeyFetch indicates a failure to fetch the public key for a decrypter.
type ErrDecrypterKeyFetch struct {
	KeyID string
	Err   error
}

// Error implements the error interface.
func (e *ErrDecrypterKeyFetch) Error() string {
	return "quicbaas decrypter: failed to fetch public key for key " + e.KeyID + ": " + e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *ErrDecrypterKeyFetch) Unwrap() error {
	return e.Err
}

// ErrDecrypterDecrypt indicates a failure during a decryption operation.
type ErrDecrypterDecrypt struct {
	KeyID string
	Err   error
}

// Error implements the error interface.
func (e *ErrDecrypterDecrypt) Error() string {
	return "quicbaas decrypter: decryption failed for key " + e.KeyID + ": " + e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *ErrDecrypterDecrypt) Unwrap() error {
	return e.Err
}

// ErrParsePublicKey indicates a failure to parse a DER-encoded public key.
type ErrParsePublicKey struct {
	KeyID string
	Err   error
}

// Error implements the error interface.
func (e *ErrParsePublicKey) Error() string {
	return "quicbaas: failed to parse public key for key " + e.KeyID + ": " + e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *ErrParsePublicKey) Unwrap() error {
	return e.Err
}

// ErrUnsupportedKeyType indicates that the public key type returned by
// the KMS service is not supported for the requested operation.
type ErrUnsupportedKeyType struct {
	KeyID   string
	KeyType string
}

// Error implements the error interface.
func (e *ErrUnsupportedKeyType) Error() string {
	return "quicbaas: unsupported key type " + e.KeyType + " for key " + e.KeyID
}

// QuicBaaSSigner implements crypto.Signer using a QuicBaaS-managed key.
// This enables QuicBaaS keys to be used with Go's standard library
// for TLS, x509 certificate signing, and other cryptographic operations
// that accept a crypto.Signer.
type QuicBaaSSigner struct {
	client    Client
	keyID     string
	publicKey crypto.PublicKey
}

// NewQuicBaaSSigner creates a new crypto.Signer backed by a QuicBaaS-managed key.
// It fetches the public key from the KMS service during construction so that
// the Public() method can return it without requiring a network call.
func NewQuicBaaSSigner(client Client, keyID string) (*QuicBaaSSigner, error) {
	publicKey, err := fetchPublicKey(client, keyID)
	if err != nil {
		return nil, &ErrSignerKeyFetch{KeyID: keyID, Err: err}
	}
	return &QuicBaaSSigner{
		client:    client,
		keyID:     keyID,
		publicKey: publicKey,
	}, nil
}

// Public returns the public key associated with this signer.
func (s *QuicBaaSSigner) Public() crypto.PublicKey {
	return s.publicKey
}

// Sign signs digest with the QuicBaaS-managed private key.
// The rand parameter is unused because the signing operation is performed
// remotely by the QuicBaaS server. The opts parameter is used to determine
// the hash algorithm that was applied to produce the digest.
func (s *QuicBaaSSigner) Sign(_ io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	hashAlg, ok := hashToAlgorithm[opts.HashFunc()]
	if !ok {
		return nil, &ErrSignerUnsupportedHash{Hash: opts.HashFunc()}
	}

	resp, err := s.client.Crypto().Sign(context.Background(), &sdkcrypto.SignRequest{
		KeyID:     s.keyID,
		Data:      digest,
		HashAlg:   hashAlg,
		PreHashed: true,
	})
	if err != nil {
		return nil, &ErrSignerSign{KeyID: s.keyID, Err: err}
	}

	return resp.Signature, nil
}

// QuicBaaSDecrypter implements crypto.Decrypter using a QuicBaaS-managed key.
// This enables QuicBaaS keys to be used with Go's standard library for
// TLS decryption and other operations that accept a crypto.Decrypter.
type QuicBaaSDecrypter struct {
	client    Client
	keyID     string
	publicKey crypto.PublicKey
}

// NewQuicBaaSDecrypter creates a new crypto.Decrypter backed by a QuicBaaS-managed key.
// It fetches the public key from the KMS service during construction so that
// the Public() method can return it without requiring a network call.
func NewQuicBaaSDecrypter(client Client, keyID string) (*QuicBaaSDecrypter, error) {
	publicKey, err := fetchPublicKey(client, keyID)
	if err != nil {
		return nil, &ErrDecrypterKeyFetch{KeyID: keyID, Err: err}
	}

	// Verify the key type supports decryption (RSA only)
	switch publicKey.(type) {
	case *rsa.PublicKey:
		// RSA keys support decryption
	default:
		return nil, &ErrUnsupportedKeyType{
			KeyID:   keyID,
			KeyType: publicKeyTypeName(publicKey),
		}
	}

	return &QuicBaaSDecrypter{
		client:    client,
		keyID:     keyID,
		publicKey: publicKey,
	}, nil
}

// Public returns the public key associated with this decrypter.
func (d *QuicBaaSDecrypter) Public() crypto.PublicKey {
	return d.publicKey
}

// Decrypt decrypts ciphertext with the QuicBaaS-managed private key.
// The rand parameter is unused because the decryption operation is performed
// remotely by the QuicBaaS server.
func (d *QuicBaaSDecrypter) Decrypt(_ io.Reader, ciphertext []byte, _ crypto.DecrypterOpts) ([]byte, error) {
	resp, err := d.client.Crypto().Decrypt(context.Background(), &sdkcrypto.DecryptRequest{
		KeyID:      d.keyID,
		Ciphertext: ciphertext,
	})
	if err != nil {
		return nil, &ErrDecrypterDecrypt{KeyID: d.keyID, Err: err}
	}

	return resp.Plaintext, nil
}

// fetchPublicKey retrieves and parses the public key for the given key ID
// from the QuicBaaS KMS service.
func fetchPublicKey(client Client, keyID string) (crypto.PublicKey, error) {
	resp, err := client.KMS().GetPublicKey(context.Background(), keyID)
	if err != nil {
		return nil, err
	}

	parsed, err := x509.ParsePKIXPublicKey(resp.PublicKey)
	if err != nil {
		return nil, &ErrParsePublicKey{KeyID: keyID, Err: err}
	}

	return parsed, nil
}

// publicKeyTypeName returns a human-readable name for the given public key.
func publicKeyTypeName(key crypto.PublicKey) string {
	switch key.(type) {
	case *rsa.PublicKey:
		return "RSA"
	case *ecdsa.PublicKey:
		return "ECDSA"
	case ed25519.PublicKey:
		return "Ed25519"
	default:
		return "unknown"
	}
}
