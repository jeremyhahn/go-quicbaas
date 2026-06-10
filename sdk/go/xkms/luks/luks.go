// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

// Package luks re-exports go-xkms's canonical LUKS surface — FIPS-aware
// volume lifecycle management plus the TPM 2.0 passphrase sealer that
// produces systemd-tpm2 compatible LUKS2 tokens — so downstream callers
// (go-trusted-platform's LUKS backend) reach it through the SDK path
// rather than importing go-xkms directly.
//
// All symbols are pure aliases of `xkms/sdk/go/luks`.
package luks

import xkmsluks "github.com/jeremyhahn/go-xkms/sdk/go/luks"

// =============================================================================
// Volume configuration + lifecycle
// =============================================================================

type (
	// KDFType identifies a key derivation function used by LUKS.
	KDFType = xkmsluks.KDFType

	// VolumeConfig holds the configuration for a LUKS encrypted volume.
	VolumeConfig = xkmsluks.VolumeConfig

	// Executor performs the actual LUKS volume operations.
	Executor = xkmsluks.Executor

	// Manager manages the lifecycle of a LUKS encrypted volume.
	Manager = xkmsluks.Manager
)

const (
	// KDFArgon2id selects the Argon2id KDF (non-FIPS default).
	KDFArgon2id = xkmsluks.KDFArgon2id

	// KDFPBKDF2 selects the PBKDF2-HMAC-SHA256 KDF (FIPS default).
	KDFPBKDF2 = xkmsluks.KDFPBKDF2
)

var (
	// DefaultKDF returns the FIPS-appropriate default KDF.
	DefaultKDF = xkmsluks.DefaultKDF

	// NewManager constructs a Manager from an explicit VolumeConfig.
	NewManager = xkmsluks.NewManager

	// NewFIPSAwareManager constructs a Manager with FIPS-appropriate KDF
	// defaults.
	NewFIPSAwareManager = xkmsluks.NewFIPSAwareManager
)

// =============================================================================
// TPM 2.0 passphrase sealing (systemd-tpm2 token format)
// =============================================================================

type (
	// TPMOpener opens a go-tpm transport for a sealing / unsealing operation.
	TPMOpener = xkmsluks.TPMOpener

	// TPMOpenerFunc adapts a function to the TPMOpener interface.
	TPMOpenerFunc = xkmsluks.TPMOpenerFunc

	// TPMToken is the systemd-tpm2 compatible LUKS2 token payload.
	TPMToken = xkmsluks.TPMToken

	// TPMSealResult is the richer seal output: passphrase, token JSON, and
	// raw sealed blobs.
	TPMSealResult = xkmsluks.TPMSealResult

	// TPMSealer seals and unseals LUKS volume passphrases against a TPM 2.0.
	TPMSealer = xkmsluks.TPMSealer
)

const (
	// TPMTokenType is the LUKS2 token type written for TPM-sealed keyslots.
	TPMTokenType = xkmsluks.TPMTokenType

	// DefaultTPMParentHandle is the default persistent sealing parent
	// handle (the TCG storage SRK, 0x81000001).
	DefaultTPMParentHandle = xkmsluks.DefaultTPMParentHandle
)

var (
	// NewTPMSealer constructs a TPM-backed LUKS passphrase sealer.
	NewTPMSealer = xkmsluks.NewTPMSealer

	// DefaultTPMOpener returns a TPMOpener that opens /dev/tpmrm0 on Linux.
	DefaultTPMOpener = xkmsluks.DefaultTPMOpener

	// EncodeTPMSealBlobs packs sealed public/private blobs into the single
	// base64 tpm2-blob token field.
	EncodeTPMSealBlobs = xkmsluks.EncodeTPMSealBlobs

	// DecodeTPMSealBlobs reverses EncodeTPMSealBlobs.
	DecodeTPMSealBlobs = xkmsluks.DecodeTPMSealBlobs
)

// =============================================================================
// Errors
// =============================================================================

var (
	ErrNotMounted      = xkmsluks.ErrNotMounted
	ErrAlreadyMounted  = xkmsluks.ErrAlreadyMounted
	ErrNotUnlocked     = xkmsluks.ErrNotUnlocked
	ErrAlreadyUnlocked = xkmsluks.ErrAlreadyUnlocked
	ErrInvalidConfig   = xkmsluks.ErrInvalidConfig
	ErrEmptyPassphrase = xkmsluks.ErrEmptyPassphrase
	ErrInvalidSize     = xkmsluks.ErrInvalidSize
	ErrInvalidKDF      = xkmsluks.ErrInvalidKDF
	ErrOperationFailed = xkmsluks.ErrOperationFailed
	ErrNilExecutor     = xkmsluks.ErrNilExecutor

	ErrTPMNoTransport   = xkmsluks.ErrTPMNoTransport
	ErrTPMPolicyFailed  = xkmsluks.ErrTPMPolicyFailed
	ErrTPMSealFailed    = xkmsluks.ErrTPMSealFailed
	ErrTPMUnsealFailed  = xkmsluks.ErrTPMUnsealFailed
	ErrTPMInvalidHandle = xkmsluks.ErrTPMInvalidHandle
	ErrTPMInvalidToken  = xkmsluks.ErrTPMInvalidToken
)
