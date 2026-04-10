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
	"sort"
	"strings"
	"sync"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// Algorithm constants define all supported cryptographic algorithms.
// These constants MUST match the service layer definitions in pkg/service/kms_types.go.
const (
	// Symmetric encryption algorithms using AEAD (Authenticated Encryption with Associated Data).
	AlgAES128GCM         = "AES-128-GCM"
	AlgAES192GCM         = "AES-192-GCM"
	AlgAES256GCM         = "AES-256-GCM"
	AlgChaCha20Poly1305  = "CHACHA20-POLY1305"
	AlgXChaCha20Poly1305 = "XCHACHA20-POLY1305"

	// RSA algorithms for encryption and digital signatures.
	AlgRSA2048 = "RSA-2048"
	AlgRSA3072 = "RSA-3072"
	AlgRSA4096 = "RSA-4096"

	// ECDSA algorithms for digital signatures (NIST curves).
	AlgECDSAP256 = "ECDSA-P256"
	AlgECDSAP384 = "ECDSA-P384"
	AlgECDSAP521 = "ECDSA-P521"

	// EdDSA algorithms for digital signatures (Edwards curves).
	AlgEd25519 = "ED25519"
	AlgEd448   = "ED448"

	// Key exchange algorithms (Diffie-Hellman on curves).
	AlgX25519 = "X25519"
	AlgX448   = "X448"

	// HMAC algorithms for message authentication.
	AlgHMACSHA256 = "HMAC-SHA256"
	AlgHMACSHA384 = "HMAC-SHA384"
	AlgHMACSHA512 = "HMAC-SHA512"
)

// Operation constants define cryptographic operations.
const (
	OpEncrypt = "encrypt"
	OpDecrypt = "decrypt"
	OpSign    = "sign"
	OpVerify  = "verify"
	OpWrap    = "wrap"
	OpUnwrap  = "unwrap"
	OpDerive  = "derive"
	OpRotate  = "rotate" // Key rotation - always supported for all key types
)

// AlgorithmType represents the category of cryptographic algorithm.
type AlgorithmType int

const (
	// AlgorithmTypeSymmetric represents symmetric encryption algorithms (AES, ChaCha20).
	AlgorithmTypeSymmetric AlgorithmType = iota

	// AlgorithmTypeAsymmetric represents asymmetric algorithms (RSA, ECDSA, EdDSA).
	AlgorithmTypeAsymmetric

	// AlgorithmTypeHMAC represents HMAC message authentication codes.
	AlgorithmTypeHMAC

	// AlgorithmTypeKeyExchange represents key exchange algorithms (X25519, X448).
	AlgorithmTypeKeyExchange
)

// String returns the string representation of AlgorithmType.
func (a AlgorithmType) String() string {
	switch a {
	case AlgorithmTypeSymmetric:
		return "Symmetric"
	case AlgorithmTypeAsymmetric:
		return "Asymmetric"
	case AlgorithmTypeHMAC:
		return "HMAC"
	case AlgorithmTypeKeyExchange:
		return "KeyExchange"
	default:
		return "Unknown"
	}
}

// AlgorithmInfo contains metadata and capabilities for a cryptographic algorithm.
type AlgorithmInfo struct {
	// Name is the canonical algorithm name (e.g., "AES-256-GCM")
	Name string

	// Type categorizes the algorithm (Symmetric, Asymmetric, HMAC, KeyExchange)
	Type AlgorithmType

	// KeySize is the key size in bits
	KeySize int

	// BlockSize is the cipher block size in bytes (0 for stream ciphers and asymmetric)
	BlockSize int

	// IVSize is the initialization vector/nonce size in bytes (0 if not applicable)
	IVSize int

	// TagSize is the authentication tag size in bytes for AEAD modes (0 if not applicable)
	TagSize int

	// SignatureSize is the signature size in bytes for signing algorithms (0 if not applicable)
	SignatureSize int

	// CanEncrypt indicates if the algorithm supports encryption operations
	CanEncrypt bool

	// CanSign indicates if the algorithm supports signing operations
	CanSign bool

	// CanWrap indicates if the algorithm can wrap other keys
	CanWrap bool

	// CanDerive indicates if the algorithm supports key derivation
	CanDerive bool
}

// algorithmRegistry manages the global registry of supported algorithms.
type algorithmRegistry struct {
	mu    sync.RWMutex
	algos map[string]*AlgorithmInfo
}

var registry = &algorithmRegistry{
	algos: make(map[string]*AlgorithmInfo),
}

// init registers all supported algorithms during package initialization.
func init() {
	// Symmetric encryption algorithms
	registerAlgorithm(&AlgorithmInfo{
		Name:       AlgAES128GCM,
		Type:       AlgorithmTypeSymmetric,
		KeySize:    128,
		BlockSize:  16,
		IVSize:     12,
		TagSize:    16,
		CanEncrypt: true,
		CanWrap:    true,
		CanDerive:  false,
		CanSign:    false,
	})

	registerAlgorithm(&AlgorithmInfo{
		Name:       AlgAES192GCM,
		Type:       AlgorithmTypeSymmetric,
		KeySize:    192,
		BlockSize:  16,
		IVSize:     12,
		TagSize:    16,
		CanEncrypt: true,
		CanWrap:    true,
		CanDerive:  false,
		CanSign:    false,
	})

	registerAlgorithm(&AlgorithmInfo{
		Name:       AlgAES256GCM,
		Type:       AlgorithmTypeSymmetric,
		KeySize:    256,
		BlockSize:  16,
		IVSize:     12,
		TagSize:    16,
		CanEncrypt: true,
		CanWrap:    true,
		CanDerive:  false,
		CanSign:    false,
	})

	registerAlgorithm(&AlgorithmInfo{
		Name:       AlgChaCha20Poly1305,
		Type:       AlgorithmTypeSymmetric,
		KeySize:    256,
		BlockSize:  0, // Stream cipher, no fixed block size
		IVSize:     12,
		TagSize:    16,
		CanEncrypt: true,
		CanWrap:    true,
		CanDerive:  false,
		CanSign:    false,
	})

	registerAlgorithm(&AlgorithmInfo{
		Name:       AlgXChaCha20Poly1305,
		Type:       AlgorithmTypeSymmetric,
		KeySize:    256,
		BlockSize:  0,  // Stream cipher, no fixed block size
		IVSize:     24, // Extended 192-bit nonce (vs 96-bit for ChaCha20)
		TagSize:    16,
		CanEncrypt: true,
		CanWrap:    true,
		CanDerive:  false,
		CanSign:    false,
	})

	// RSA algorithms
	registerAlgorithm(&AlgorithmInfo{
		Name:          AlgRSA2048,
		Type:          AlgorithmTypeAsymmetric,
		KeySize:       2048,
		BlockSize:     0,
		IVSize:        0,
		TagSize:       0,
		SignatureSize: 256, // 2048 bits / 8
		CanEncrypt:    true,
		CanSign:       true,
		CanWrap:       true,
		CanDerive:     false,
	})

	registerAlgorithm(&AlgorithmInfo{
		Name:          AlgRSA3072,
		Type:          AlgorithmTypeAsymmetric,
		KeySize:       3072,
		BlockSize:     0,
		IVSize:        0,
		TagSize:       0,
		SignatureSize: 384, // 3072 bits / 8
		CanEncrypt:    true,
		CanSign:       true,
		CanWrap:       true,
		CanDerive:     false,
	})

	registerAlgorithm(&AlgorithmInfo{
		Name:          AlgRSA4096,
		Type:          AlgorithmTypeAsymmetric,
		KeySize:       4096,
		BlockSize:     0,
		IVSize:        0,
		TagSize:       0,
		SignatureSize: 512, // 4096 bits / 8
		CanEncrypt:    true,
		CanSign:       true,
		CanWrap:       true,
		CanDerive:     false,
	})

	// ECDSA algorithms
	registerAlgorithm(&AlgorithmInfo{
		Name:          AlgECDSAP256,
		Type:          AlgorithmTypeAsymmetric,
		KeySize:       256,
		BlockSize:     0,
		IVSize:        0,
		TagSize:       0,
		SignatureSize: 64, // r + s (32 bytes each)
		CanEncrypt:    false,
		CanSign:       true,
		CanWrap:       false,
		CanDerive:     false,
	})

	registerAlgorithm(&AlgorithmInfo{
		Name:          AlgECDSAP384,
		Type:          AlgorithmTypeAsymmetric,
		KeySize:       384,
		BlockSize:     0,
		IVSize:        0,
		TagSize:       0,
		SignatureSize: 96, // r + s (48 bytes each)
		CanEncrypt:    false,
		CanSign:       true,
		CanWrap:       false,
		CanDerive:     false,
	})

	registerAlgorithm(&AlgorithmInfo{
		Name:          AlgECDSAP521,
		Type:          AlgorithmTypeAsymmetric,
		KeySize:       521,
		BlockSize:     0,
		IVSize:        0,
		TagSize:       0,
		SignatureSize: 132, // r + s (66 bytes each)
		CanEncrypt:    false,
		CanSign:       true,
		CanWrap:       false,
		CanDerive:     false,
	})

	// EdDSA algorithms
	registerAlgorithm(&AlgorithmInfo{
		Name:          AlgEd25519,
		Type:          AlgorithmTypeAsymmetric,
		KeySize:       256,
		BlockSize:     0,
		IVSize:        0,
		TagSize:       0,
		SignatureSize: 64,
		CanEncrypt:    false,
		CanSign:       true,
		CanWrap:       false,
		CanDerive:     false,
	})

	registerAlgorithm(&AlgorithmInfo{
		Name:          AlgEd448,
		Type:          AlgorithmTypeAsymmetric,
		KeySize:       448,
		BlockSize:     0,
		IVSize:        0,
		TagSize:       0,
		SignatureSize: 114,
		CanEncrypt:    false,
		CanSign:       true,
		CanWrap:       false,
		CanDerive:     false,
	})

	// Key exchange algorithms
	registerAlgorithm(&AlgorithmInfo{
		Name:       AlgX25519,
		Type:       AlgorithmTypeKeyExchange,
		KeySize:    256,
		BlockSize:  0,
		IVSize:     0,
		TagSize:    0,
		CanEncrypt: false,
		CanSign:    false,
		CanWrap:    false,
		CanDerive:  true,
	})

	registerAlgorithm(&AlgorithmInfo{
		Name:       AlgX448,
		Type:       AlgorithmTypeKeyExchange,
		KeySize:    448,
		BlockSize:  0,
		IVSize:     0,
		TagSize:    0,
		CanEncrypt: false,
		CanSign:    false,
		CanWrap:    false,
		CanDerive:  true,
	})

	// HMAC algorithms
	registerAlgorithm(&AlgorithmInfo{
		Name:          AlgHMACSHA256,
		Type:          AlgorithmTypeHMAC,
		KeySize:       256,
		BlockSize:     0,
		IVSize:        0,
		TagSize:       32,
		SignatureSize: 32,
		CanEncrypt:    false,
		CanSign:       true,
		CanWrap:       false,
		CanDerive:     true,
	})

	registerAlgorithm(&AlgorithmInfo{
		Name:          AlgHMACSHA384,
		Type:          AlgorithmTypeHMAC,
		KeySize:       384,
		BlockSize:     0,
		IVSize:        0,
		TagSize:       48,
		SignatureSize: 48,
		CanEncrypt:    false,
		CanSign:       true,
		CanWrap:       false,
		CanDerive:     true,
	})

	registerAlgorithm(&AlgorithmInfo{
		Name:          AlgHMACSHA512,
		Type:          AlgorithmTypeHMAC,
		KeySize:       512,
		BlockSize:     0,
		IVSize:        0,
		TagSize:       64,
		SignatureSize: 64,
		CanEncrypt:    false,
		CanSign:       true,
		CanWrap:       false,
		CanDerive:     true,
	})
}

// registerAlgorithm adds an algorithm to the registry.
func registerAlgorithm(info *AlgorithmInfo) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.algos[info.Name] = info
}

// GetAlgorithmInfo retrieves algorithm information by name.
// Performs case-insensitive lookup by normalizing to uppercase, since all
// registered algorithm names use uppercase (e.g., "CHACHA20-POLY1305").
// Returns an error if the algorithm is not found.
func GetAlgorithmInfo(name string) (*AlgorithmInfo, error) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()

	normalized := strings.ToUpper(name)
	if info, exists := registry.algos[normalized]; exists {
		return info, nil
	}

	return nil, api.NewUnknownAlgorithmError("kms.GetAlgorithmInfo", name)
}

// IsValidAlgorithm checks if an algorithm name is registered.
// Performs case-insensitive lookup by normalizing to uppercase.
func IsValidAlgorithm(name string) bool {
	registry.mu.RLock()
	defer registry.mu.RUnlock()

	normalized := strings.ToUpper(name)
	_, exists := registry.algos[normalized]
	return exists
}

// ListAlgorithms returns a sorted list of all registered algorithm names.
func ListAlgorithms() []string {
	registry.mu.RLock()
	defer registry.mu.RUnlock()

	names := make([]string, 0, len(registry.algos))
	for name := range registry.algos {
		names = append(names, name)
	}

	sort.Strings(names)
	return names
}

// ListAlgorithmsByType returns a sorted list of algorithm names filtered by type.
func ListAlgorithmsByType(algType AlgorithmType) []string {
	registry.mu.RLock()
	defer registry.mu.RUnlock()

	names := make([]string, 0)
	for name, info := range registry.algos {
		if info.Type == algType {
			names = append(names, name)
		}
	}

	sort.Strings(names)
	return names
}

// ValidateKeySize validates that the provided key size matches the algorithm's requirements.
// keyBytes is the number of key bytes (not bits).
func ValidateKeySize(algorithm string, keyBytes int) error {
	info, err := GetAlgorithmInfo(algorithm)
	if err != nil {
		return err
	}

	expectedBytes := info.KeySize / 8
	if keyBytes != expectedBytes {
		return api.NewKeySizeError("kms.ValidateKeySize", algorithm, expectedBytes, keyBytes)
	}

	return nil
}

// CanAlgorithmPerform checks if an algorithm supports a specific operation.
// operation should be one of: "encrypt", "decrypt", "sign", "verify", "wrap", "unwrap", "derive".
func CanAlgorithmPerform(algorithm, operation string) bool {
	info, err := GetAlgorithmInfo(algorithm)
	if err != nil {
		return false
	}

	switch operation {
	case OpEncrypt, OpDecrypt:
		return info.CanEncrypt
	case OpSign, OpVerify:
		return info.CanSign
	case OpWrap, OpUnwrap:
		return info.CanWrap
	case OpDerive:
		return info.CanDerive
	case OpRotate:
		return true // Key rotation is always supported - it's a key management operation
	default:
		return false
	}
}

// GetKeySizeBytes returns the key size in bytes for an algorithm.
// Returns an error if the algorithm is not found.
func GetKeySizeBytes(algorithm string) (int, error) {
	info, err := GetAlgorithmInfo(algorithm)
	if err != nil {
		return 0, err
	}

	return info.KeySize / 8, nil
}

// GetIVSize returns the IV/nonce size in bytes for an algorithm.
// Returns an error if the algorithm is not found.
func GetIVSize(algorithm string) (int, error) {
	info, err := GetAlgorithmInfo(algorithm)
	if err != nil {
		return 0, err
	}

	return info.IVSize, nil
}

// GetTagSize returns the authentication tag size in bytes for an algorithm.
// Returns an error if the algorithm is not found.
func GetTagSize(algorithm string) (int, error) {
	info, err := GetAlgorithmInfo(algorithm)
	if err != nil {
		return 0, err
	}

	return info.TagSize, nil
}

// GetSignatureSize returns the signature size in bytes for a signing algorithm.
// Returns an error if the algorithm is not found or doesn't support signing.
func GetSignatureSize(algorithm string) (int, error) {
	info, err := GetAlgorithmInfo(algorithm)
	if err != nil {
		return 0, err
	}

	if !info.CanSign {
		return 0, api.NewAlgorithmSigningUnsupportedError("kms.GetSignatureSize", algorithm)
	}

	return info.SignatureSize, nil
}
