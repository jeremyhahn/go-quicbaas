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

package server

import (
	"errors"
	"log/slog"
	"strconv"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// ErrNilFactor is the cause carried by XKMSCryptoError when cfg.Factors
// contains a nil AuthFactorPlugin. A nil factor is a composition-time
// programming error; surfacing it as a typed cause avoids the nil-pointer
// panic that f.Name() would otherwise raise.
var ErrNilFactor = errors.New("quicbaas/server: nil auth factor plugin in XKMSCryptoConfig.Factors")

// XKMSCryptoConfig describes the hardware backends and Factor
// implementations that quicbaas injects into the embedded qrdb server at
// startup. Every field is optional: when nil or zero, qrdb uses software-only
// defaults (SoftwareStrategy / SoftwareBackend / empty factor registry).
// qrdb's default factor registry is empty — mTLS is not an AuthFactorPlugin
// (mTLS login is handled natively by qrdb's LoginResolver). Hardware-capable
// deployments populate the fields with go-xkms-backed implementations to
// enable TPM2 / PKCS#11 / cloud KMS strategies, hardware PIN backends, and
// FIDO2 / PIV / PKCS#11 factor adapters.
type XKMSCryptoConfig struct {
	// Logger is used to log fallback decisions and by the SoftwareBackend
	// constructor. Nil defaults to slog.Default().
	Logger *slog.Logger

	// PINStorage backs the default qrdb qrdbsdk.SoftwareBackend. Nil
	// yields an in-memory-only PIN backend (suitable for tests; not
	// persistent across daemon restarts).
	PINStorage qrdbsdk.PINStorage

	// PINBackend overrides the default qrdbsdk.SoftwareBackend with a
	// caller-supplied PINBackend (typically a TPM2 or PKCS#11 backend
	// from go-xkms). When set, PINStorage is ignored.
	PINBackend qrdbsdk.PINBackend

	// StrategyFactories registers hardware-backed sealing strategies
	// (TPM2, PKCS#11, AWS KMS, GCP KMS, Azure KV, Vault) into the
	// composed registry alongside the software defaults. The map key is
	// the canonical strategy name (e.g. xkmssdk.StrategyTPM2); the value
	// is a factory that constructs the strategy on demand.
	StrategyFactories map[qrdbsdk.StrategyID]qrdbsdk.StrategyFactory

	// Factors are auth-factor implementations (typically FIDO2 / PIV /
	// PKCS#11 from go-xkms/pkg/factor) registered into the composed
	// FactorRegistry. qrdb's default registry is empty; consumers that
	// want a software mTLS factor or any hardware factor must register
	// them here.
	Factors []qrdbsdk.AuthFactorPlugin
}

// XKMSCryptoOptions builds the qrdb server options that inject go-xkms
// hardware crypto into the embedded engine: the seal-strategy registry,
// the PIN backend, and the auth-factor registry. It is the single seam
// where hardware crypto reaches qrdb's server.New; software-only
// deployments are simply the empty-cfg case. It guarantees a non-nil
// StrategyRegistry, FactorRegistry, and PINBackend so the qrdb server
// does not have to build software-only fallbacks itself.
//
// Defaults (when cfg is nil or fields are zero):
//   - StrategyRegistry: qrdbsdk.NewDefaultStrategyRegistry() —
//     pre-populated with Software + None strategies.
//   - PINBackend: qrdbsdk.SoftwareBackend over an in-memory store using
//     qrdbsdk.AutoDetectHashConfig() (PBKDF2 in FIPS mode, Argon2id
//     otherwise).
//   - FactorRegistry: empty (caller can add a software mTLS factor or
//     hardware factors via cfg.Factors).
//
// When cfg.StrategyFactories carries hardware factories, they are
// registered into the StrategyRegistry alongside the software defaults
// so a single registry covers every advertised strategy. When
// cfg.PINBackend is non-nil, it overrides the default; otherwise the
// SoftwareBackend is constructed against cfg.PINStorage. When
// cfg.Factors carries Factor implementations, they are registered into
// the FactorRegistry.
//
// The returned slice is independent of the input cfg and safe to pass
// directly to a qrdbsdk-composed qrdb server.
func XKMSCryptoOptions(cfg *XKMSCryptoConfig) ([]qrdbsdk.ServerOption, error) {
	if cfg == nil {
		cfg = &XKMSCryptoConfig{}
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	strategyReg := qrdbsdk.NewDefaultStrategyRegistry()
	for name, factory := range cfg.StrategyFactories {
		if err := strategyReg.Register(name, factory); err != nil {
			return nil, &XKMSCryptoError{Stage: "register strategy " + string(name), Cause: err}
		}
	}

	pinBackend := cfg.PINBackend
	if pinBackend == nil {
		sb, err := qrdbsdk.NewSoftwareBackend(cfg.PINStorage, qrdbsdk.AutoDetectHashConfig())
		if err != nil {
			return nil, &XKMSCryptoError{Stage: "construct default qrdbsdk.SoftwareBackend", Cause: err}
		}
		pinBackend = sb
	}

	factorReg := qrdbsdk.NewFactorRegistry()
	for i, f := range cfg.Factors {
		if f == nil {
			return nil, &XKMSCryptoError{Stage: "register factor #" + strconv.Itoa(i), Cause: ErrNilFactor}
		}
		if err := factorReg.Register(f); err != nil {
			return nil, &XKMSCryptoError{Stage: "register factor " + f.Name(), Cause: err}
		}
	}

	logger.Debug("quicbaas: built qrdb xkms crypto options",
		slog.String("pin_strategy", string(pinBackend.Strategy())),
		slog.Int("registered_strategies", len(strategyReg.ListAvailable())),
		slog.Int("registered_factors", len(factorReg.List())),
	)

	return []qrdbsdk.ServerOption{
		qrdbsdk.WithStrategyRegistry(strategyReg),
		qrdbsdk.WithPINBackend(pinBackend),
		qrdbsdk.WithFactorRegistry(factorReg),
	}, nil
}

// XKMSCryptoError describes a failure in XKMSCryptoOptions. Stage names
// the wiring step that failed; Cause is the underlying error.
type XKMSCryptoError struct {
	Stage string
	Cause error
}

// Error implements the error interface.
func (e *XKMSCryptoError) Error() string {
	if e.Cause == nil {
		return "quicbaas/server: xkms crypto options failed at " + e.Stage
	}
	return "quicbaas/server: xkms crypto options failed at " + e.Stage + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is / errors.As traversal.
func (e *XKMSCryptoError) Unwrap() error { return e.Cause }
