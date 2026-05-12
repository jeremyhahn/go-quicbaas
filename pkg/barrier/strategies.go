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

package barrier

import (
	qrdbseal "github.com/jeremyhahn/go-qrdb/pkg/seal"
)

// StrategyKind identifies the operator-selected sealing strategy. The
// values map directly onto the qrdb seal strategy IDs so that the
// configured kind round-trips through Status responses.
type StrategyKind string

const (
	// StrategyDisabled indicates that seal is turned off entirely. The
	// lifecycle layer skips barrier construction in this mode.
	StrategyDisabled StrategyKind = "disabled"

	// StrategySoftware is the passphrase-based software strategy. The
	// passphrase is supplied at unseal time via the operator API; the
	// strategy chain stores a deferred software strategy that binds the
	// passphrase only when an unseal/initialize call arrives.
	StrategySoftware StrategyKind = "software"

	// StrategyShamir is the Shamir Secret Sharing strategy. The wrapping
	// strategy is software (deferred); shamir kicks in for share-based
	// initialize and unseal flows. Both strategies are appended to the
	// chain so the barrier can select the active one at runtime.
	StrategyShamir StrategyKind = "shamir"

	// StrategyTPM2 binds the root key to a local TPM 2.0 device. The
	// TPM2 strategy is provided by go-xkms; QuicBaaS injects it through
	// the lifecycle layer so the barrier package itself does not depend
	// on TPM hardware libraries.
	StrategyTPM2 StrategyKind = "tpm2"

	// StrategyPKCS11 wraps the root key with a PKCS#11-resident KEK.
	StrategyPKCS11 StrategyKind = "pkcs11"

	// StrategyAWSKMS wraps the root key under an AWS KMS CMK.
	StrategyAWSKMS StrategyKind = "awskms"

	// StrategyGCPKMS wraps the root key under a GCP KMS key.
	StrategyGCPKMS StrategyKind = "gcpkms"

	// StrategyAzureKV wraps the root key under an Azure Key Vault key.
	StrategyAzureKV StrategyKind = "azurekv"

	// StrategyVault wraps the root key with a HashiCorp Vault Transit key.
	StrategyVault StrategyKind = "vault"
)

// validStrategies enumerates every operator-selectable kind. Used for
// CLI/REST validation and documentation.
var validStrategies = map[StrategyKind]struct{}{
	StrategyDisabled: {},
	StrategySoftware: {},
	StrategyShamir:   {},
	StrategyTPM2:     {},
	StrategyPKCS11:   {},
	StrategyAWSKMS:   {},
	StrategyGCPKMS:   {},
	StrategyAzureKV:  {},
	StrategyVault:    {},
}

// IsValidStrategy returns true if kind is a recognized sealing strategy.
func IsValidStrategy(kind StrategyKind) bool {
	_, ok := validStrategies[kind]
	return ok
}

// UnknownStrategyError is returned when a strategy kind is not recognized.
type UnknownStrategyError struct {
	Kind string
}

// Error implements the error interface.
func (e *UnknownStrategyError) Error() string {
	return "barrier: unknown sealing strategy: " + e.Kind
}

// StrategyUnsupportedError is returned when a strategy is recognized but
// the current build does not include a constructor for it (cloud KMS
// providers and PKCS#11 in builds without the pkcs11 build tag).
type StrategyUnsupportedError struct {
	Kind string
}

// Error implements the error interface.
func (e *StrategyUnsupportedError) Error() string {
	return "barrier: sealing strategy not available in this build: " + e.Kind
}

// StrategyConstructionError wraps an error returned from a SealingStrategy
// constructor. The wrapped error preserves the underlying cause for
// operator diagnostics.
type StrategyConstructionError struct {
	Kind string
	Err  error
}

// Error implements the error interface.
func (e *StrategyConstructionError) Error() string {
	return "barrier: failed to construct " + e.Kind + " strategy: " + e.Err.Error()
}

// Unwrap returns the underlying error so errors.Is/As keep working.
func (e *StrategyConstructionError) Unwrap() error { return e.Err }

// StrategyChainConfig captures the inputs needed to assemble a strategy
// chain for seal.NewStorageBarrier. The lifecycle layer populates it from
// configuration; the resulting strategies are passed positionally into
// NewStorageBarrier alongside the BarrierConfig.
type StrategyChainConfig struct {
	// Kind selects the active sealing strategy.
	Kind StrategyKind

	// Backend is the storage backend the barrier persists state into.
	// Must be non-nil for non-disabled kinds; the disabled kind ignores
	// it.
	Backend qrdbseal.StorageBackend

	// ShamirThreshold is the M in M-of-N for the shamir kind. Ignored
	// for other kinds.
	ShamirThreshold int

	// ShamirTotal is the N in M-of-N for the shamir kind. Ignored for
	// other kinds.
	ShamirTotal int

	// TPM2Strategy is an injected TPM2 strategy from go-xkms. When kind
	// is "tpm2" this MUST be non-nil. Constructed by the lifecycle
	// layer because the TPM sealer lives in go-xkms.
	TPM2Strategy qrdbseal.SealingStrategy

	// PKCS11Config configures the PKCS#11 strategy when kind is
	// "pkcs11". The user PIN is loaded from the named env var at
	// runtime; the config never carries it directly.
	PKCS11Config *qrdbseal.PKCS11Config
}

// BuildStrategyChain returns the ordered list of SealingStrategy values
// to pass to seal.NewStorageBarrier for the configured kind. The
// "disabled" kind returns an empty slice so the lifecycle layer can skip
// barrier construction entirely.
//
// Software: a single deferred software strategy that binds the
// passphrase per-call.
//
// Shamir: the underlying Shamir strategy plus a deferred software
// strategy fallback so that legacy non-shamir unseal paths still work.
//
// TPM2: the injected TPM2 strategy plus a deferred software strategy
// fallback (the qrdb barrier preference order picks TPM2 first when
// available).
//
// PKCS11: the PKCS#11 strategy plus a deferred software strategy
// fallback. Returns StrategyUnsupportedError when the build does not
// include the pkcs11 tag (NewPKCS11Strategy is the stub form).
//
// AWSKMS / GCPKMS / AzureKV / Vault: not yet implemented upstream;
// returns StrategyUnsupportedError. When the upstream constructors
// land, this function is the single integration point.
func BuildStrategyChain(cfg *StrategyChainConfig) ([]qrdbseal.SealingStrategy, error) {
	if cfg == nil {
		return nil, &UnknownStrategyError{Kind: ""}
	}
	switch cfg.Kind {
	case StrategyDisabled:
		return nil, nil

	case StrategySoftware:
		soft := qrdbseal.NewDeferredSoftwareStrategy()
		return []qrdbseal.SealingStrategy{soft}, nil

	case StrategyShamir:
		if cfg.Backend == nil {
			return nil, &StrategyConstructionError{Kind: string(StrategyShamir), Err: errBackendRequired}
		}
		if cfg.ShamirThreshold < 2 || cfg.ShamirTotal < cfg.ShamirThreshold {
			return nil, &StrategyConstructionError{Kind: string(StrategyShamir), Err: errShamirParams}
		}
		shamirStrat, err := qrdbseal.NewShamirStrategy(cfg.Backend, cfg.ShamirThreshold, cfg.ShamirTotal)
		if err != nil {
			return nil, &StrategyConstructionError{Kind: string(StrategyShamir), Err: err}
		}
		soft := qrdbseal.NewDeferredSoftwareStrategy()
		return []qrdbseal.SealingStrategy{shamirStrat, soft}, nil

	case StrategyTPM2:
		if cfg.TPM2Strategy == nil {
			return nil, &StrategyConstructionError{Kind: string(StrategyTPM2), Err: errTPM2Required}
		}
		soft := qrdbseal.NewDeferredSoftwareStrategy()
		return []qrdbseal.SealingStrategy{cfg.TPM2Strategy, soft}, nil

	case StrategyPKCS11:
		if cfg.PKCS11Config == nil {
			return nil, &StrategyConstructionError{Kind: string(StrategyPKCS11), Err: errPKCS11ConfigRequired}
		}
		hsmStrat, err := qrdbseal.NewPKCS11Strategy(cfg.PKCS11Config)
		if err != nil {
			return nil, &StrategyConstructionError{Kind: string(StrategyPKCS11), Err: err}
		}
		if !hsmStrat.Available() {
			return nil, &StrategyUnsupportedError{Kind: string(StrategyPKCS11)}
		}
		soft := qrdbseal.NewDeferredSoftwareStrategy()
		return []qrdbseal.SealingStrategy{hsmStrat, soft}, nil

	case StrategyAWSKMS, StrategyGCPKMS, StrategyAzureKV, StrategyVault:
		// Cloud KMS strategies are not yet exposed by the upstream
		// seal package. When the constructors land in
		// go-quicraft/pkg/seal, switch this branch to construct them
		// here. Returning a typed error keeps the surface stable.
		return nil, &StrategyUnsupportedError{Kind: string(cfg.Kind)}

	default:
		return nil, &UnknownStrategyError{Kind: string(cfg.Kind)}
	}
}

// errBackendRequired indicates a strategy needs a non-nil storage backend.
var errBackendRequired = &constErr{"barrier: storage backend is required"}

// errShamirParams indicates threshold/total parameters violate the
// Shamir constraints.
var errShamirParams = &constErr{"barrier: shamir threshold must be >= 2 and total >= threshold"}

// errTPM2Required indicates the lifecycle layer did not inject a TPM2
// strategy when configuring kind=tpm2.
var errTPM2Required = &constErr{"barrier: TPM2 strategy must be injected by the lifecycle layer when kind=tpm2"}

// errPKCS11ConfigRequired indicates the operator selected pkcs11 without
// supplying configuration.
var errPKCS11ConfigRequired = &constErr{"barrier: PKCS#11 configuration is required when kind=pkcs11"}

// constErr is a tiny typed error so we never fall back to fmt.Errorf for
// barrier-level invariants.
type constErr struct{ msg string }

// Error implements the error interface.
func (e *constErr) Error() string { return e.msg }
