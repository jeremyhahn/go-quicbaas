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

// Package xkms provides the integration layer between QuicBaaS and go-xkms.
// This file provides centralized RNG (Random Number Generator) functionality.
// ALL random number generation for cryptographic operations in QuicBaaS MUST
// use these functions to ensure consistent, configurable, and secure random sources.
package xkms

import (
	"io"
	"math/big"
	"sync"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// ErrRNGNotInitialized is returned when RNG operations are attempted before initialization.
var ErrRNGNotInitialized = &api.RNGNotInitializedError{}

// ErrRNGFailed is returned when random number generation fails.
var ErrRNGFailed = &api.RNGError{}

// RNGMode specifies which RNG source to use.
type RNGMode = xkmssdk.RandMode

// RNG mode constants - re-exported for convenience.
const (
	// RNGModeAuto automatically selects the best available RNG.
	// Preference order: TPM2 > PKCS#11 > Software.
	RNGModeAuto RNGMode = xkmssdk.RandModeAuto

	// RNGModeSoftware uses crypto/rand (stdlib secure random).
	RNGModeSoftware RNGMode = xkmssdk.RandModeSoftware

	// RNGModeTPM2 uses Trusted Platform Module 2.0 hardware RNG.
	RNGModeTPM2 RNGMode = xkmssdk.RandModeTPM2

	// RNGModePKCS11 uses PKCS#11 hardware security module RNG.
	RNGModePKCS11 RNGMode = xkmssdk.RandModePKCS11
)

// RNGConfig contains RNG configuration options.
type RNGConfig = xkmssdk.RandConfig

// RNG state - protected by initMu from xkms.go.
var (
	rngResolver xkmssdk.RandResolver
	rngMu       sync.RWMutex
)

// InitializeRNG initializes the RNG resolver with the specified configuration.
// This should be called after Initialize() if custom RNG configuration is needed.
// If not called explicitly, RNG is initialized with auto mode on first use.
func InitializeRNG(config *RNGConfig) error {
	rngMu.Lock()
	defer rngMu.Unlock()

	if rngResolver != nil {
		return ErrAlreadyInitialized
	}

	resolver, err := xkmssdk.RandNewResolver(config)
	if err != nil {
		return &api.RNGInitializationError{Cause: err}
	}

	rngResolver = resolver
	return nil
}

// ensureRNG ensures the RNG is initialized, using auto mode if not.
func ensureRNG() error {
	rngMu.RLock()
	if rngResolver != nil {
		rngMu.RUnlock()
		return nil
	}
	rngMu.RUnlock()

	// Need to initialize
	rngMu.Lock()
	defer rngMu.Unlock()

	// Double-check after acquiring write lock
	if rngResolver != nil {
		return nil
	}

	// Initialize with auto mode
	resolver, err := xkmssdk.RandNewResolver(xkmssdk.RandModeAuto)
	if err != nil {
		return &api.RNGInitializationError{Detail: "failed to auto-initialize RNG", Cause: err}
	}

	rngResolver = resolver
	return nil
}

// Rand generates n cryptographically secure random bytes using the configured RNG.
// This is the primary function for generating random data in QuicBaaS.
//
// Example usage:
//
//	// Generate 32 random bytes for a key
//	key, err := xkms.Rand(32)
//	if err != nil {
//	    return err
//	}
//
//	// Generate 12 bytes for a nonce
//	nonce, err := xkms.Rand(12)
func Rand(n int) ([]byte, error) {
	if err := ensureRNG(); err != nil {
		return nil, &api.RNGError{Cause: err}
	}

	rngMu.RLock()
	defer rngMu.RUnlock()

	data, err := rngResolver.Rand(n)
	if err != nil {
		return nil, &api.RNGError{Cause: err}
	}

	return data, nil
}

// RandInt returns a uniform random value in [0, max). It panics if max <= 0.
// This is the cryptographically secure alternative to math/rand.Int.
//
// Example usage:
//
//	// Generate a random serial number for a certificate
//	max := new(big.Int).Lsh(big.NewInt(1), 128)
//	serial, err := xkms.RandInt(max)
func RandInt(max *big.Int) (*big.Int, error) {
	if max == nil || max.Sign() <= 0 {
		return nil, &api.RNGError{Detail: "max must be > 0"}
	}

	if err := ensureRNG(); err != nil {
		return nil, &api.RNGError{Cause: err}
	}

	rngMu.RLock()
	defer rngMu.RUnlock()

	// Calculate the byte size needed
	byteLen := (max.BitLen() + 7) / 8

	// Generate random bytes
	buf, err := rngResolver.Rand(byteLen)
	if err != nil {
		return nil, &api.RNGError{Cause: err}
	}

	// Convert to big.Int
	n := new(big.Int).SetBytes(buf)

	// Mask to correct bit length to avoid bias
	n = n.Mod(n, max)

	// Return the result (which is always in valid range with Mod)
	return n, nil
}

// Reader returns an io.Reader that generates random bytes from the configured RNG.
// This can be used as a drop-in replacement for rand.Reader in crypto operations.
//
// Example usage:
//
//	// Use with ecdsa.GenerateKey
//	key, err := ecdsa.GenerateKey(elliptic.P256(), xkms.Reader())
//
//	// Use with x509.CreateCertificate
//	cert, err := x509.CreateCertificate(xkms.Reader(), template, parent, pub, priv)
func Reader() io.Reader {
	return &rngReader{}
}

// rngReader implements io.Reader using the configured RNG.
type rngReader struct{}

// Read fills p with random bytes from the configured RNG.
func (r *rngReader) Read(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	data, err := Rand(len(p))
	if err != nil {
		return 0, err
	}

	copy(p, data)
	return len(p), nil
}

// RNGAvailable returns true if the RNG is available and ready for use.
func RNGAvailable() bool {
	if err := ensureRNG(); err != nil {
		return false
	}

	rngMu.RLock()
	defer rngMu.RUnlock()

	return rngResolver.Available()
}

// CloseRNG closes the RNG and releases any resources.
// This is called automatically by Close().
func CloseRNG() error {
	rngMu.Lock()
	defer rngMu.Unlock()

	if rngResolver == nil {
		return nil
	}

	err := rngResolver.Close()
	rngResolver = nil
	return err
}

// ResetRNG resets the RNG state (for testing).
func ResetRNG() {
	rngMu.Lock()
	defer rngMu.Unlock()

	if rngResolver != nil {
		_ = rngResolver.Close()
	}
	rngResolver = nil
}
