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
	"os"
	"path/filepath"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// pinDirName is the sub-directory of the data dir that holds the persistent
// software PIN records (Argon2id/PBKDF2 hashes). Matches the xKey convention.
const pinDirName = "pin"

// pinPlatformStateFile is the file under <dataDir>/pin where the TPM
// PlatformKeyStore persists its PIN/SRK state. It records only whether a
// PlatformSRK was created and confirmed — never a PIN value, which lives solely
// as the PlatformSRK's TPM password auth.
const pinPlatformStateFile = "platform-pin-state.json"

// PINBackendError describes a failure constructing the daemon PIN backend.
// Stage names the step that failed; Cause is the underlying error.
type PINBackendError struct {
	Stage string
	Cause error
}

// Error implements the error interface.
func (e *PINBackendError) Error() string {
	if e.Cause == nil {
		return "quicbaas/server: pin backend " + e.Stage + " failed"
	}
	return "quicbaas/server: pin backend " + e.Stage + " failed: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is / errors.As traversal.
func (e *PINBackendError) Unwrap() error { return e.Cause }

// PINRequiresTPMSealError reports that the operator explicitly selected the
// TPM2 PIN backend (crypto.subsystems.pin.backend: tpm2, or an inherited
// crypto.backend: tpm2) but the active seal strategy is NOT TPM-backed, so
// there is no open TPM to share. PIN verification must reuse the seal
// strategy's single TPM open (a second open hangs a single-session emulator and
// wastes a command channel on real hardware); without a TPM seal there is
// nothing to share. This is a hard, fail-closed configuration error — an
// explicit hardware demand is never silently downgraded to software (mirroring
// the KMS backend policy). The fix is to also configure seal.strategy: tpm2.
type PINRequiresTPMSealError struct {
	// SealStrategy is the configured seal strategy that is not TPM-backed.
	SealStrategy string
}

// Error implements the error interface.
func (e *PINRequiresTPMSealError) Error() string {
	return "quicbaas/server: pin backend tpm2 requires a TPM2 seal strategy " +
		"(set seal.strategy: tpm2 or auto with a provisioned TPM); the active " +
		"seal strategy is " + quoteOrEmpty(e.SealStrategy) +
		", which exposes no shared TPM open for in-hardware PIN verification"
}

// PINBackendUnsupportedError reports that the resolved PIN backend type is a
// real backend but not one the PIN subsystem supports. The PIN backend
// verifies a barrier-unlock credential; only software and tpm2 are wired.
// PKCS#11 and cloud KMS backends have no PIN-verification semantics here.
type PINBackendUnsupportedError struct {
	Backend config.BackendType
}

// Error implements the error interface.
func (e *PINBackendUnsupportedError) Error() string {
	return "quicbaas/server: pin backend " + string(e.Backend) +
		" is not supported; valid values are \"\"/auto, software, tpm2"
}

// TPMPINEnrollmentError reports that first-boot enrollment of the TPM
// PlatformSRK (binding the operator passphrase as its TPM auth value) failed.
// It is fail-closed: an explicit tpm2 PIN demand with a supplied operator
// passphrase is never silently downgraded when the TPM rejects enrollment.
type TPMPINEnrollmentError struct {
	Cause error
}

// Error implements the error interface.
func (e *TPMPINEnrollmentError) Error() string {
	if e.Cause == nil {
		return "quicbaas/server: TPM PlatformSRK first-boot enrollment failed"
	}
	return "quicbaas/server: TPM PlatformSRK first-boot enrollment failed: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is / errors.As traversal.
func (e *TPMPINEnrollmentError) Unwrap() error { return e.Cause }

// quoteOrEmpty renders s as a quoted token, or the word "software" when empty
// (the empty seal strategy resolves to the software passphrase default).
func quoteOrEmpty(s string) string {
	if s == "" {
		return "\"\" (software passphrase default)"
	}
	return "\"" + s + "\""
}

// softwarePINProvider supplies the persistent storage + hash parameters for the
// software PIN fallback. When the data dir is empty (tests, no persistent
// backend) it yields a nil store so qrdb constructs an in-memory-only backend.
//
// The software backend is the ONLY PIN backend that persists a hash, and it
// does so with Argon2id/PBKDF2 (salted) under <dataDir>/pin — because it has no
// hardware to delegate verification + lockout to. Hardware backends (TPM2,
// PKCS#11) verify in-device and never persist a hash.
type softwarePINProvider struct {
	dataDir string
}

// Storage opens the persistent PIN store, or returns a nil store (in-memory)
// when no data dir is configured.
func (p softwarePINProvider) Storage() (qrdbsdk.PINStorage, error) {
	if p.dataDir == "" {
		return nil, nil
	}
	pinDir := filepath.Join(p.dataDir, pinDirName)
	if err := os.MkdirAll(pinDir, 0o700); err != nil {
		return nil, &PINBackendError{Stage: "mkdir pin dir", Cause: err}
	}
	fs, err := xkmssdk.NewFileStorageBackend(pinDir)
	if err != nil {
		return nil, &PINBackendError{Stage: "open pin file storage", Cause: err}
	}
	adapter, err := xkmssdk.NewPINStoreAdapter(fs)
	if err != nil {
		return nil, &PINBackendError{Stage: "wrap pin storage adapter", Cause: err}
	}
	return adapter, nil
}

// HashConfig returns the auto-detected hash config (PBKDF2 under GOFIPS140,
// Argon2id otherwise), matching qrdb's software backend default.
func (p softwarePINProvider) HashConfig() qrdbsdk.PINHashConfig {
	return qrdbsdk.AutoDetectHashConfig()
}

// sealSharedTPMPINProvider adapts the active TPM2 seal backend's already-open
// TPM into a pinselect.TPMProvider (xkmssdk.PINTPMProvider). It is the P3
// single-TPM-open bridge: PlatformAuth builds a PlatformKeyStore on the SHARED
// open TPM (backend.TPM()) — it NEVER calls NewTPM2Backend, so the device is
// opened exactly once for both seal and PIN.
//
// PIN verification delegates to a PlatformSRK whose password auth IS the user
// PIN — a key DISTINCT from the seal SSRK (so setting/changing the PIN never
// disturbs the root-key seal). The provider reports available only when that
// PlatformSRK is already initialized: quicbaasd does NOT provision a PlatformSRK
// at boot (the qrdb PINBackend contract verifies an existing auth value, it
// cannot bootstrap one, and boot-time TPM provisioning is unreliable). When no
// PlatformSRK is present the provider reports unavailable and pinselect falls
// back to the persistent software backend, logging the reason. A PlatformSRK is
// established out-of-band (e.g. by the xKey enrollment wizard against the same
// TPM + KeyDir).
type sealSharedTPMPINProvider struct {
	// backend is the open TPM2 seal backend whose single TPM session is shared.
	backend *xkmssdk.TPM2Backend
	// cfg supplies cfg.Seal.TPM2 so the PlatformKeyStore is built with the same
	// device/handle/key-dir parameters as the seal backend.
	cfg *config.Config
	// pinStatePath is where the PlatformKeyStore persists its (PIN-free) state.
	pinStatePath string
	logger       *slog.Logger
}

// platformKeyStore returns the PlatformKeyStore attached to the shared seal TPM,
// building one on the same open session when none is cached. NewPlatformKeyStore
// does NOT open the device — it operates over the TPM handle passed in — so this
// never costs a second TPM open. It returns nil when no shared TPM is available.
func (p sealSharedTPMPINProvider) platformKeyStore(logger *slog.Logger) (xkmssdk.PlatformKeyStorer, error) {
	if p.backend == nil {
		return nil, nil
	}
	tpm := p.backend.TPM()
	if tpm == nil {
		return nil, nil
	}
	if pks := tpm.PlatformKeyStore(); pks != nil {
		return pks, nil
	}
	return xkmssdk.NewPlatformKeyStore(
		logger,
		tpm,
		p.backend.KeyBackend(),
		p.platformTPMConfig(),
		p.pinStatePath,
	)
}

// PlatformAuth implements xkmssdk.PINTPMProvider. It returns the PlatformSRK
// auth provider over the shared open TPM when a PlatformSRK is initialized, or
// (nil, false) to make pinselect fall through to software.
func (p sealSharedTPMPINProvider) PlatformAuth() (xkmssdk.TPMPlatformAuthProvider, bool) {
	logger := p.logger
	if logger == nil {
		logger = slog.Default()
	}

	pks, err := p.platformKeyStore(logger)
	if err != nil {
		logger.Warn("PIN backend: could not build TPM PlatformKeyStore on the shared seal TPM; falling back to software",
			slog.String("error", err.Error()))
		return nil, false
	}
	if pks == nil {
		return nil, false
	}

	if !pks.IsInitialized() {
		logger.Warn("PIN backend: TPM2 seal backend present but no PlatformSRK is initialized; " +
			"falling back to software (no operator passphrase was supplied to enroll one at " +
			"first boot — set QUICBAAS_SEAL_PASSPHRASE, or enroll out-of-band via xKey)")
		return nil, false
	}

	concrete, ok := pks.(*xkmssdk.PlatformKeyStore)
	if !ok {
		logger.Warn("PIN backend: shared TPM PlatformKeyStore is not the expected concrete type; falling back to software")
		return nil, false
	}
	logger.Info("PIN backend: using TPM2 PlatformSRK over the shared seal TPM open (in-hardware verification with native DA lockout)")
	return concrete, true
}

// enroll performs a one-shot first-boot enrollment of the PlatformSRK over the
// shared seal TPM, binding operatorPIN as its TPM password auth. It is
// idempotent across boots: an already-enrolled SRK (or one whose auth already
// matches) is treated as success.
func (p sealSharedTPMPINProvider) enroll(operatorPIN []byte, logger *slog.Logger) error {
	pks, err := p.platformKeyStore(logger)
	if err != nil {
		return err
	}
	if pks == nil {
		return nil // no shared TPM; caller handles the fail-closed policy
	}
	// The first argument is the TPM owner hierarchy auth, not a PKCS#11-style
	// security-officer PIN: the Platform SRK is a persistent object in the owner
	// hierarchy, so creating it needs that auth whenever the hierarchy carries
	// one. quicbaasd has none to supply -- provisionTPM2Backend provisions with
	// tpm.Provision(nil), which leaves every hierarchy unauthenticated, and no
	// config field carries an owner auth for the seal TPM. An empty value leaves
	// the hierarchy auth configured for the SRK in force. A TPM provisioned
	// out-of-band with an owner auth therefore still fails closed here, reporting
	// xkmssdk.ErrPlatformSRKHierarchyAuth through TPMPINEnrollmentError.
	if err := pks.EnrollPlatformSRK("", string(operatorPIN)); err != nil {
		if errors.Is(err, xkmssdk.ErrPlatformSRKAlreadyEnrolled) {
			return nil
		}
		return err
	}
	return nil
}

// platformTPMConfig derives the TPM config for the PlatformKeyStore from the
// same seal TPM2 settings, so the PlatformSRK uses the configured device, key
// directory, and handles. A nil PlatformSRK sub-config is tolerated by go-xkms
// (it falls back to its default platform SRK handle).
func (p sealSharedTPMPINProvider) platformTPMConfig() *xkmssdk.TPMConfig {
	tpmCfg := p.cfg.Seal.TPM2
	backendCfg := &xkmssdk.TPM2BackendConfig{
		Device:          tpmCfg.Device,
		KeyDir:          tpmCfg.KeyDir,
		UseSimulator:    tpmCfg.UseSimulator,
		SimulatorHost:   tpmCfg.SimulatorHost,
		SimulatorPort:   tpmCfg.SimulatorPort,
		SRKHandle:       tpmCfg.SRKHandle,
		PlatformPolicy:  tpmCfg.PlatformPolicy,
		PlatformPCR:     tpmCfg.PCR,
		PlatformPCRBank: tpmCfg.PCRBank,
	}
	return backendCfg.ToTPMConfig()
}

// newSealSharedTPMPINProvider builds the TPM PIN provider that shares the seal
// strategy's open TPM, or returns nil when no TPM2 seal backend is active (so
// there is nothing to share). It is the seam initStandaloneQRDBNode /
// initClusteredQRDBNode use after the seal strategy has been built.
func newSealSharedTPMPINProvider(sharedTPM *xkmssdk.TPM2Backend, cfg *config.Config, logger *slog.Logger) xkmssdk.PINTPMProvider {
	if sharedTPM == nil || cfg == nil {
		return nil
	}
	return newSealSharedTPMPINProviderConcrete(sharedTPM, cfg, logger)
}

// newSealSharedTPMPINProviderConcrete builds the concrete provider value. It is
// used by both the interface constructor (above) and first-boot enrollment,
// which needs the concrete type to call enroll.
func newSealSharedTPMPINProviderConcrete(sharedTPM *xkmssdk.TPM2Backend, cfg *config.Config, logger *slog.Logger) sealSharedTPMPINProvider {
	return sealSharedTPMPINProvider{
		backend:      sharedTPM,
		cfg:          cfg,
		pinStatePath: pinPlatformStatePath(cfg),
		logger:       logger,
	}
}

// pinPlatformStatePath returns the PlatformKeyStore state-file path under the
// configured data dir.
func pinPlatformStatePath(cfg *config.Config) string {
	return filepath.Join(cfg.Storage.DataDir, pinDirName, pinPlatformStateFile)
}

// enrollTPMPlatformSRKFirstBoot drives a one-time enrollment of the TPM
// PlatformSRK at first boot when the PIN backend is tpm2, binding the
// operator-supplied passphrase as the PlatformSRK's TPM auth value so the TPM
// PIN path works end-to-end. It runs over the shared seal TPM open (never a
// second TPM handle) and is idempotent across boots.
//
// It is a no-op (returns nil) when:
//   - the resolved PIN backend is not tpm2 (software deployments), or
//   - no TPM2 seal is active (sharedTPM nil) — buildPINBackend then emits the
//     fail-closed PINRequiresTPMSealError, exactly as in P3, or
//   - no operator passphrase was supplied (nothing to enroll); the PIN backend
//     falls back to software and logs the reason.
//
// A genuine TPM enrollment failure (e.g. a too-short PIN, or the owner
// hierarchy carrying unknown auth) is fail-closed via TPMPINEnrollmentError.
func enrollTPMPlatformSRKFirstBoot(cfg *config.Config, sharedTPM *xkmssdk.TPM2Backend, operatorPIN []byte, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	if resolvePINBackendType(cfg) != config.BackendTypeTPM2 {
		return nil
	}
	if sharedTPM == nil {
		return nil
	}
	if len(operatorPIN) == 0 {
		logger.Warn("PIN backend: tpm2 PIN selected but no operator passphrase supplied; " +
			"cannot enroll the TPM PlatformSRK at first boot (set QUICBAAS_SEAL_PASSPHRASE); " +
			"PIN verification will fall back to software until a PlatformSRK is enrolled")
		return nil
	}
	provider := newSealSharedTPMPINProviderConcrete(sharedTPM, cfg, logger)
	if err := provider.enroll(operatorPIN, logger); err != nil {
		return &TPMPINEnrollmentError{Cause: err}
	}
	logger.Info("PIN backend: TPM PlatformSRK enrolled/confirmed at first boot over the shared seal TPM open")
	return nil
}

// resolvePINBackendType resolves the effective PIN backend type from config,
// applying the precedence (highest first):
//
//  1. crypto.subsystems.pin.backend  — per-subsystem override
//  2. crypto.backend                 — process-wide crypto default (applied
//     when the subsystem spec is unset/"auto")
//  3. ""/auto                        — software
//
// Unlike KMS/CA, the PIN backend NEVER auto-selects hardware: ""/auto always
// resolve to software (the safe default that boots without a TPM). Only an
// EXPLICIT tpm2 (named directly on the PIN subsystem or inherited from an
// explicit crypto.backend: tpm2) opts in to the TPM PIN path. This mirrors the
// opt-in seal-strategy policy: hardware is deliberate, software is the default.
func resolvePINBackendType(cfg *config.Config) config.BackendType {
	if cfg == nil {
		return config.BackendTypeSoftware
	}
	bt := cfg.Crypto.Subsystems.PIN.Backend
	if config.IsAutoBackend(bt) && cfg.Crypto.Backend != "" {
		bt = cfg.Crypto.Backend
	}
	if config.IsAutoBackend(bt) {
		return config.BackendTypeSoftware
	}
	return bt
}

// buildPINBackend selects the daemon PIN backend via the shared go-xkms
// pinselect decision point, honoring the configured backend type.
//
//   - software / "" / auto: a PERSISTENT software backend (Argon2id/PBKDF2 under
//     <dataDir>/pin). This is the default and is byte-for-byte the prior
//     behavior — existing deployments are unaffected.
//   - tpm2: the TPM PIN provider (tpmProvider), which shares the active TPM2
//     seal strategy's single open TPM and verifies the PIN against a PlatformSRK
//     in hardware with native dictionary-attack lockout. tpmProvider is nil when
//     no TPM2 seal strategy is active; in that case this returns a typed
//     PINRequiresTPMSealError (fail-closed: an explicit hardware demand is never
//     silently downgraded). When tpmProvider is present but reports unavailable
//     (no PlatformSRK initialized), pinselect falls back to software.
//
// tpmProvider is injected by the caller so the TPM open is shared with the seal
// strategy (and so unit tests can supply a fake without a live TPM).
func buildPINBackend(cfg *config.Config, logger *slog.Logger, tpmProvider xkmssdk.PINTPMProvider) (qrdbsdk.PINBackend, error) {
	if logger == nil {
		logger = slog.Default()
	}
	var dataDir string
	if cfg != nil {
		dataDir = cfg.Storage.DataDir
	}

	selectCfg := xkmssdk.PINSelectConfig{
		Software: softwarePINProvider{dataDir: dataDir},
		Logger:   logger,
	}

	switch pinType := resolvePINBackendType(cfg); pinType {
	case config.BackendTypeSoftware:
		// Software-only preference order: never attempt TPM even if a provider
		// was passed. The explicit software choice opts out of hardware PIN.
		selectCfg.Preference = []qrdbsdk.PINStrategyID{qrdbsdk.PINStrategySoftware}

	case config.BackendTypeTPM2:
		if tpmProvider == nil {
			// PIN=tpm2 but no TPM2 seal strategy is active: nothing to share.
			var sealStrategy string
			if cfg != nil {
				sealStrategy = cfg.Seal.Strategy
			}
			return nil, &PINRequiresTPMSealError{SealStrategy: sealStrategy}
		}
		// TPM first, software fallback (used when the provider reports the
		// PlatformSRK is not yet initialized — see sealSharedTPMPINProvider).
		selectCfg.TPM = tpmProvider
		selectCfg.Preference = []qrdbsdk.PINStrategyID{
			qrdbsdk.PINStrategyTPM2,
			qrdbsdk.PINStrategySoftware,
		}

	default:
		return nil, &PINBackendUnsupportedError{Backend: pinType}
	}

	backend, err := xkmssdk.SelectPINBackend(selectCfg)
	if err != nil {
		return nil, &PINBackendError{Stage: "select pin backend", Cause: err}
	}
	return backend, nil
}
