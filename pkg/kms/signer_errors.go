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

package kms

import (
	"errors"
	"fmt"
)

// ErrServiceNotInitialized indicates the KMSService receiver is nil.
// This is a programmer error: Signer must be called on an initialized
// service.
var ErrServiceNotInitialized = errors.New("kms service is not initialized")

// ErrSignerNotInitialized indicates Sign was called on a zero-value
// kmsSigner. This should never occur in practice — Signer always
// returns a fully-populated instance — but keeps the Sign path panic
// free for defensive callers.
var ErrSignerNotInitialized = errors.New("kms signer is not initialized")

// UnsupportedKeyTypeError indicates the public key type cannot be used
// with the KMS signer wrapper. Only RSA, ECDSA, and Ed25519 keys have
// well-defined signature algorithms in this layer.
type UnsupportedKeyTypeError struct{}

// Error implements the error interface.
func (e *UnsupportedKeyTypeError) Error() string {
	return "unsupported public key type for kms signer"
}

// UnsupportedCurveError indicates the ECDSA curve is not supported by
// the signer layer. The BitSize is the curve size reported by the
// key's Params().BitSize.
type UnsupportedCurveError struct {
	BitSize int
}

// Error implements the error interface.
func (e *UnsupportedCurveError) Error() string {
	return fmt.Sprintf("unsupported ecdsa curve: P-%d", e.BitSize)
}
