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

import "errors"

// Sentinel errors for KMS operations.
var (
	// ErrKeyIDEmpty indicates a keyID parameter is empty.
	ErrKeyIDEmpty = errors.New("keyID cannot be empty")

	// ErrKeyAttributesNil indicates key attributes are nil.
	ErrKeyAttributesNil = errors.New("key attributes cannot be nil")

	// ErrKeyCNEmpty indicates key common name is empty.
	ErrKeyCNEmpty = errors.New("key CN (common name) cannot be empty")

	// ErrKeyMaterialEmpty indicates key material is empty.
	ErrKeyMaterialEmpty = errors.New("key material cannot be empty")

	// ErrMaterialEmpty indicates material parameter is empty.
	ErrMaterialEmpty = errors.New("material cannot be empty")

	// ErrMetadataNil indicates metadata is nil.
	ErrMetadataNil = errors.New("metadata cannot be nil")

	// ErrMetadataTypeMismatch indicates metadata is not the expected type.
	ErrMetadataTypeMismatch = errors.New("metadata must be *KeyMetadata")

	// ErrKeyIDRequired indicates key_id is required.
	ErrKeyIDRequired = errors.New("key_id is required")

	// ErrOptionsNil indicates options are nil.
	ErrOptionsNil = errors.New("options cannot be nil")

	// ErrConfigNil indicates config is nil.
	ErrConfigNil = errors.New("config cannot be nil")

	// ErrRegistryRequired indicates a registry is required.
	ErrRegistryRequired = errors.New("Registry is required; use BackendFactory to create and populate the registry")

	// ErrRegistryEmpty indicates the registry must contain at least one backend.
	ErrRegistryEmpty = errors.New("Registry must contain at least one backend")

	// ErrRegistryUnavailable indicates the registry is not available.
	ErrRegistryUnavailable = errors.New("registry not available")

	// ErrNoBackendAvailable indicates no available backend for the operation.
	ErrNoBackendAvailable = errors.New("no available backend for operation")

	// ErrNoFailoverBackends indicates no backends available for failover.
	ErrNoFailoverBackends = errors.New("no backends available for failover")

	// ErrPlaintextEmpty indicates plaintext is empty.
	ErrPlaintextEmpty = errors.New("plaintext cannot be empty")

	// ErrCiphertextEmpty indicates ciphertext is empty.
	ErrCiphertextEmpty = errors.New("ciphertext cannot be empty")

	// ErrDigestEmpty indicates digest is empty.
	ErrDigestEmpty = errors.New("digest cannot be empty")

	// ErrSignatureEmpty indicates signature is empty.
	ErrSignatureEmpty = errors.New("signature cannot be empty")

	// ErrKeyBytesPositive indicates keyBytes must be positive.
	ErrKeyBytesPositive = errors.New("keyBytes must be positive")
)
