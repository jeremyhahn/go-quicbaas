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

// Package luks re-exports go-xkms's canonical LUKS surface: the FIPS-aware
// volume configuration and lifecycle manager, its production go-luks2
// backed Executor, and the TPM 2.0 passphrase sealer with its configurable
// storage parent (Platform SRK, the default, or the Shared SRK for
// systemd-cryptsetup compatibility). Downstream callers
// (go-trusted-platform's LUKS backend) reach it through the SDK path rather
// than importing go-xkms directly.
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

	// LibExecutor is the production Executor, implemented on go-luks2.
	LibExecutor = xkmsluks.LibExecutor
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

	// NewLibExecutor constructs the production Executor.
	NewLibExecutor = xkmsluks.NewLibExecutor

	// IsLUKSVolume reports whether a file begins with the LUKS2 magic bytes.
	IsLUKSVolume = xkmsluks.IsLUKSVolume

	// IsMounted reports whether a path is currently a mount point.
	IsMounted = xkmsluks.IsMounted

	// IsUnlocked reports whether a LUKS device-mapper mapping exists.
	IsUnlocked = xkmsluks.IsUnlocked

	// EnsureKeyringAvailable proves the kernel keyring is usable by this
	// process before a volume key is handled.
	EnsureKeyringAvailable = xkmsluks.EnsureKeyringAvailable
)

// =============================================================================
// TPM 2.0 passphrase sealing
// =============================================================================

type (
	// SealParent identifies which TPM storage hierarchy a LUKS passphrase
	// seal is parented to: the Platform SRK (default) or the Shared SRK.
	SealParent = xkmsluks.SealParent

	// TPMSealerConfig configures a TPMSealer's storage parent and PCR bank.
	TPMSealerConfig = xkmsluks.TPMSealerConfig

	// TPMSealUnsealer is the subset of pkg/tpm2.TPM2's behavior TPMSealer
	// depends on.
	TPMSealUnsealer = xkmsluks.TPMSealUnsealer

	// TPMSealResult is the sealed passphrase plus the LUKS2 token to persist
	// in the volume's header and the raw sealed blobs.
	TPMSealResult = xkmsluks.TPMSealResult

	// TPMSealer seals and unseals LUKS volume passphrases against a TPM 2.0.
	TPMSealer = xkmsluks.TPMSealer
)

const (
	// SealParentPSRK seals under go-xkms's own Platform Storage Root Key,
	// bound to go-xkms's platform PCR policy. This is the default.
	SealParentPSRK = xkmsluks.SealParentPSRK

	// SealParentSSRK seals under the TCG-standard Shared Storage Root Key
	// with empty parent auth, for systemd-cryptsetup compatibility.
	SealParentSSRK = xkmsluks.SealParentSSRK

	// TPMTokenType is the LUKS2 token type written for a SealParentSSRK
	// seal, matching systemd-cryptsetup's "systemd-tpm2" token.
	TPMTokenType = xkmsluks.TPMTokenType

	// XKMSTPMTokenType is the LUKS2 token type written for a SealParentPSRK
	// seal.
	XKMSTPMTokenType = xkmsluks.XKMSTPMTokenType
)

var (
	// ParseSealParent parses the config-file spelling of a seal parent.
	ParseSealParent = xkmsluks.ParseSealParent

	// NewTPMSealer constructs a TPM-backed LUKS passphrase sealer.
	NewTPMSealer = xkmsluks.NewTPMSealer

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

	ErrVolumeAlreadyExists = xkmsluks.ErrVolumeAlreadyExists
	ErrVolumeNotFound      = xkmsluks.ErrVolumeNotFound

	ErrTPMNoTransport              = xkmsluks.ErrTPMNoTransport
	ErrTPMPolicyFailed             = xkmsluks.ErrTPMPolicyFailed
	ErrTPMSealFailed               = xkmsluks.ErrTPMSealFailed
	ErrTPMUnsealFailed             = xkmsluks.ErrTPMUnsealFailed
	ErrTPMInvalidToken             = xkmsluks.ErrTPMInvalidToken
	ErrInvalidSealParent           = xkmsluks.ErrInvalidSealParent
	ErrInvalidPCRBank              = xkmsluks.ErrInvalidPCRBank
	ErrTPMSSRKUnsealUnsupported    = xkmsluks.ErrTPMSSRKUnsealUnsupported
	ErrTPMPCRSelectionNotSupported = xkmsluks.ErrTPMPCRSelectionNotSupported
	ErrKeyringUnavailable          = xkmsluks.ErrKeyringUnavailable
)

type (
	// SealParentInvalidError reports a SealParent value or raw config
	// string that names neither "psrk" nor "ssrk".
	SealParentInvalidError = xkmsluks.SealParentInvalidError

	// ExecutorError reports which production LUKS operation failed and
	// against which path or device.
	ExecutorError = xkmsluks.ExecutorError

	// KeyringError reports a failed kernel keyring preflight.
	KeyringError = xkmsluks.KeyringError
)
