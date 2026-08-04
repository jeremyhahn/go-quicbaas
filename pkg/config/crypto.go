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

package config

// BackendAuto requests hardware-prefer auto-detection. It is a sentinel
// BackendType value (not a real go-xkms backend) that the BackendResolver
// treats identically to the empty string: TPM2 if a device is present, else
// software. It is intentionally NOT a member of validBackendTypes — cloud and
// HSM backends are never auto-selected.
const BackendAuto BackendType = "auto"

// BackendSpec identifies a backend type and an optional key reference for one
// crypto site (KMS service, CA signing key, PIN backend, barrier AEAD). It is
// defined in pkg/config — not pkg/server — so config structs can embed it
// without creating a pkg/config -> pkg/server import cycle. pkg/server aliases
// it as server.BackendSpec; there is exactly one definition.
//
// Backend "" and "auto" are synonymous: the hardware-prefer policy applies.
type BackendSpec struct {
	// Backend selects the backend type. Valid values:
	//   ""/"auto"   hardware-prefer auto-detection (TPM2 if present, else software)
	//   "software"  go-xkms software backend
	//   "tpm2"      TPM 2.0 (always compiled in)
	//   "pkcs11"    PKCS#11 HSM (requires -tags pkcs11)
	//   "awskms"    AWS Key Management Service (requires -tags awskms)
	//   "gcpkms"    Google Cloud KMS (requires -tags gcpkms)
	//   "azurekv"   Azure Key Vault (requires -tags azurekv)
	//   "vault"     HashiCorp Vault Transit (requires -tags vault)
	//   "quantum"   Post-quantum (ML-DSA/ML-KEM, pure Go, no tag)
	Backend BackendType `mapstructure:"backend" yaml:"backend" json:"backend"`

	// KeyRef is a backend-specific key identifier. Its meaning depends on
	// Backend (tpm2: persistent-handle label; pkcs11: CKA_LABEL; awskms: key
	// ARN/ID/alias; gcpkms: full resource name; azurekv: key name; vault:
	// Transit key name; software: key label/directory prefix). Empty means the
	// backend's own default applies.
	KeyRef string `mapstructure:"key_ref" yaml:"key_ref" json:"key_ref"`
}

// CryptoConfig is the process-wide cryptographic backend policy. It provides a
// single default plus per-subsystem overrides for every crypto site in
// quicbaas. The zero value selects the software backend everywhere, which is
// byte-for-byte identical to the historical behavior — existing deployments
// with no `crypto:` block are unaffected.
type CryptoConfig struct {
	// Backend is the process-wide default BackendType applied when a
	// subsystem's own spec is absent or "auto". "" and "auto" are treated
	// identically: TPM2 if present, else software. Cloud and HSM backends
	// require explicit naming; they are never auto-selected.
	Backend BackendType `mapstructure:"backend" yaml:"backend" json:"backend"`

	// KeyRef is the process-wide default key label/identifier. A non-empty
	// subsystem KeyRef overrides it.
	KeyRef string `mapstructure:"key_ref" yaml:"key_ref" json:"key_ref"`

	// Subsystems carries per-subsystem backend overrides. A zero-value
	// BackendSpec means "inherit from the process default".
	Subsystems CryptoSubsystemsConfig `mapstructure:"subsystems" yaml:"subsystems" json:"subsystems"`
}

// CryptoSubsystemsConfig holds per-subsystem backend overrides.
type CryptoSubsystemsConfig struct {
	// KMS overrides the backend for the quicbaas KMS service (key management,
	// signing, symmetric encryption). When Backend is "", inherits from
	// CryptoConfig.Backend.
	KMS BackendSpec `mapstructure:"kms" yaml:"kms" json:"kms"`

	// CA overrides the backend for CA signing-key storage. The TLS server key
	// always remains software (it must be exportable for transport); only the
	// CA signing key is eligible for hardware storage.
	CA BackendSpec `mapstructure:"ca" yaml:"ca" json:"ca"`

	// PIN overrides the backend for the PIN backend (barrier credential
	// store). Valid: "" (auto), "software", "tpm2".
	PIN BackendSpec `mapstructure:"pin" yaml:"pin" json:"pin"`

	// BarrierAEAD overrides the AEAD provider for the data-plane barrier's
	// symmetric key wrapping. "auto" follows the hardware-prefer policy. Only
	// software and tpm2 are supported by the resolver in this phase.
	BarrierAEAD BackendSpec `mapstructure:"barrier_aead" yaml:"barrier_aead" json:"barrier_aead"`
}

// DefaultCryptoConfig returns the default crypto policy: the zero value, which
// resolves to software everywhere. It exists so callers can express intent
// explicitly; CryptoConfig{} is equivalent.
func DefaultCryptoConfig() CryptoConfig {
	return CryptoConfig{}
}

// IsValidBackendType reports whether bt is a known go-xkms backend type. The
// auto/empty sentinels are NOT valid backend types — use IsAutoBackend for
// those. Exported so pkg/server's resolver can distinguish an unknown backend
// (typo / unsupported value) from a known-but-not-compiled one.
func IsValidBackendType(bt BackendType) bool {
	_, ok := validBackendTypes[bt]
	return ok
}

// IsAutoBackend reports whether bt requests hardware-prefer auto-detection,
// i.e. it is the empty string or "auto".
func IsAutoBackend(bt BackendType) bool {
	return bt == "" || bt == BackendAuto
}

// validateCryptoBackend rejects a backend value that is neither auto/empty nor
// a known backend type. It returns a typed *ValidationError naming the field.
func validateCryptoBackend(field string, bt BackendType) error {
	if IsAutoBackend(bt) {
		return nil
	}
	if !IsValidBackendType(bt) {
		return &ValidationError{
			Field:   field,
			Message: "unknown crypto backend type: " + string(bt),
		}
	}
	return nil
}

// Validate checks the crypto policy. Empty/auto and every known backend type
// are accepted; any other value yields a typed *ValidationError. The zero
// value validates successfully (software everywhere).
func (c *CryptoConfig) Validate() error {
	if err := validateCryptoBackend("crypto.backend", c.Backend); err != nil {
		return err
	}
	if err := validateCryptoBackend("crypto.subsystems.kms.backend", c.Subsystems.KMS.Backend); err != nil {
		return err
	}
	if err := validateCryptoBackend("crypto.subsystems.ca.backend", c.Subsystems.CA.Backend); err != nil {
		return err
	}
	if err := validateCryptoBackend("crypto.subsystems.pin.backend", c.Subsystems.PIN.Backend); err != nil {
		return err
	}
	if err := validateCryptoBackend("crypto.subsystems.barrier_aead.backend", c.Subsystems.BarrierAEAD.Backend); err != nil {
		return err
	}
	return nil
}
