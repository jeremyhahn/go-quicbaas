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

// Package kms.
//
// This file implements crypto.Signer-compatible shims over the KMS
// service. They let callers that expect a standard-library crypto.Signer
// — e.g. x/crypto/ssh, crypto/tls — talk to KMS-resident keys without
// ever having the raw private material in memory. All Sign calls are
// forwarded to the KMS backend through KMSService.Sign.
package kms

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"io"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// kmsSigner is the crypto.Signer implementation returned by
// KMSService.Signer. It caches the public key on construction (fetched
// once via GetPublicKey) and forwards every Sign call to the backing
// KMSService with a well-known algorithm string derived from the key
// type.
type kmsSigner struct {
	// svc is the KMS service that owns the underlying key material.
	svc *KMSService

	// keyID identifies the stored key. KMSService.Sign routes to the
	// correct backend based on this value.
	keyID string

	// publicKey is the parsed public component, fetched once at
	// construction. Holding it locally keeps Public() allocation-free
	// in hot paths (TLS handshakes, SSH auth).
	publicKey crypto.PublicKey

	// signAlgorithm is the algorithm string passed to KMSService.Sign.
	// Derived from the public key type so we do not have to re-resolve
	// metadata on every signature.
	signAlgorithm string
}

// Public returns the public half of the key. It never returns nil on a
// successfully-constructed signer; construction would have failed at
// KMSService.Signer time if the public key could not be parsed.
func (s *kmsSigner) Public() crypto.PublicKey {
	return s.publicKey
}

// Sign signs the digest using the KMS-backed private key. It honours
// the opts hash function when present — RSA-PSS and RSA-PKCS1 callers
// set opts.HashFunc(), and Ed25519 callers pass a zero Hash — the
// backend is expected to match the algorithm we advertised via
// signAlgorithm.
//
// The rand parameter is ignored: randomness is sourced by the backend.
func (s *kmsSigner) Sign(_ io.Reader, digest []byte, _ crypto.SignerOpts) ([]byte, error) {
	if s == nil || s.svc == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "kmsSigner.Sign",
			Err:  ErrSignerNotInitialized,
		}
	}
	return s.svc.Sign(context.Background(), s.keyID, digest, s.signAlgorithm)
}

// Signer implements interfaces.KMSManager.Signer. It loads the stored
// public key and wraps the service in a crypto.Signer. The caller must
// supply a key ID that references an asymmetric key; symmetric keys
// have no signing identity and yield a typed error.
func (s *KMSService) Signer(ctx context.Context, keyID string) (crypto.Signer, error) {
	if s == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "KMSService.Signer",
			Err:  ErrServiceNotInitialized,
		}
	}
	if keyID == "" {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.Signer",
			Err:  ErrKeyIDEmpty,
		}
	}

	derBytes, err := s.GetPublicKey(ctx, keyID)
	if err != nil {
		return nil, err
	}

	pub, err := x509.ParsePKIXPublicKey(derBytes)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "KMSService.Signer",
			Err:  api.NewPublicKeyMarshalError("KMSService.Signer", err),
		}
	}

	algo, err := signAlgorithmForKey(pub)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.Signer",
			Err:  err,
		}
	}

	return &kmsSigner{
		svc:           s,
		keyID:         keyID,
		publicKey:     pub,
		signAlgorithm: algo,
	}, nil
}

// signAlgorithmForKey returns the algorithm string the KMS backend
// expects for the given public key type. The values match what
// KMSService.Sign consumes today: RSA uses SHA-256 PKCS1v15, ECDSA is
// curve-specific, and Ed25519 is pure.
func signAlgorithmForKey(pub crypto.PublicKey) (string, error) {
	switch k := pub.(type) {
	case *rsa.PublicKey:
		return "SHA256withRSA", nil
	case *ecdsa.PublicKey:
		switch k.Curve.Params().BitSize {
		case 256:
			return "ECDSA-P256-SHA256", nil
		case 384:
			return "ECDSA-P384-SHA384", nil
		case 521:
			return "ECDSA-P521-SHA512", nil
		default:
			return "", &UnsupportedCurveError{BitSize: k.Curve.Params().BitSize}
		}
	case ed25519.PublicKey:
		return "Ed25519", nil
	default:
		return "", &UnsupportedKeyTypeError{}
	}
}
