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
	"crypto/x509"
	"errors"
	"log/slog"
	"os"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// This file is the registry and dispatch layer that wires go-xkms hardware-
// and cloud-backed sealing strategies into the quicbaas barrier composer.
//
// The build-tag-free TPM2 strategy is registered here directly (see init()).
// The five build-tagged strategies — awskms, gcpkms, azurekv, vault, pkcs11 —
// live in their own files (e.g. seal_factory_awskms.go) guarded by a build
// tag matching their strategy ID. Each such file registers its factory into
// hardwareStrategyFactories from its own init() via
// registerHardwareStrategyFactory. When the matching tag is not compiled, the
// factory is simply absent from the map and buildHardwareSealStrategies
// returns a StrategyNotCompiledError that tells the operator to rebuild with
// the appropriate -tags value.
//
// The crypto is never reimplemented here: each factory constructs a go-xkms
// backend from the operator config and wraps it with the corresponding
// go-xkms SealingStrategy (qrdbsdk.SealingStrategy alias).

// hardwareStrategyFactory constructs a single hardware/cloud SealingStrategy
// from the full server config. The factory is responsible for reading its own
// sub-config (e.g. cfg.Seal.TPM2) and building the go-xkms backend + strategy.
type hardwareStrategyFactory func(cfg *config.Config) (qrdbsdk.SealingStrategy, error)

// hardwareStrategyFactories maps a hardware/cloud strategy ID to its factory.
// It is populated at package-init time: the TPM2 factory is registered by this
// file's init(); each build-tagged backend registers its own factory from a
// guarded file's init(). Reads happen only after all init() functions have
// run, so the map needs no synchronization.
var hardwareStrategyFactories = map[qrdbsdk.StrategyID]hardwareStrategyFactory{}

// hardwareStrategyIDs is the set of strategy IDs that are hardware- or
// cloud-backed and therefore dispatched through hardwareStrategyFactories
// rather than buildSealStrategies. A strategy in this set with no registered
// factory yields a StrategyNotCompiledError.
var hardwareStrategyIDs = map[qrdbsdk.StrategyID]struct{}{
	xkmssdk.StrategyTPM2:    {},
	xkmssdk.StrategyPKCS11:  {},
	xkmssdk.StrategyAWSKMS:  {},
	xkmssdk.StrategyGCPKMS:  {},
	xkmssdk.StrategyAzureKV: {},
	xkmssdk.StrategyVault:   {},
}

// registerHardwareStrategyFactory installs factory under id. It is called from
// the init() of each strategy file (including this one for TPM2). Re-
// registering the same id panics: a duplicate registration is a programming
// error (two files claiming the same strategy), not a runtime condition.
func registerHardwareStrategyFactory(id qrdbsdk.StrategyID, factory hardwareStrategyFactory) {
	if factory == nil {
		panic("server: nil hardware strategy factory for " + string(id))
	}
	if _, exists := hardwareStrategyFactories[id]; exists {
		panic("server: duplicate hardware strategy factory for " + string(id))
	}
	hardwareStrategyFactories[id] = factory
}

// isHardwareStrategy reports whether id is dispatched through the hardware
// factory registry rather than the software/none/shamir path.
func isHardwareStrategy(id qrdbsdk.StrategyID) bool {
	_, ok := hardwareStrategyIDs[id]
	return ok
}

// StrategyBuildError reports a failure constructing a hardware/cloud sealing
// strategy from its backend config. Backend names the go-xkms backend that
// failed; Cause is the underlying error.
type StrategyBuildError struct {
	Backend string
	Cause   error
}

// Error implements the error interface.
func (e *StrategyBuildError) Error() string {
	if e.Cause == nil {
		return "seal strategy: building " + e.Backend + " backend failed"
	}
	return "seal strategy: building " + e.Backend + " backend failed: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is/As traversal.
func (e *StrategyBuildError) Unwrap() error { return e.Cause }

// StrategyNotCompiledError reports that a hardware/cloud strategy was selected
// in the config but its factory is not registered — i.e. the binary was built
// without the build tag that compiles that backend in. The error message names
// the -tags value needed to enable it.
type StrategyNotCompiledError struct {
	Strategy qrdbsdk.StrategyID
}

// Error implements the error interface.
func (e *StrategyNotCompiledError) Error() string {
	return "seal strategy: " + string(e.Strategy) +
		" was selected but is not compiled into this binary; rebuild with -tags " +
		string(e.Strategy)
}

// buildHardwareSealStrategies resolves the hardware/cloud sealing strategy
// selected by cfg.Seal.Strategy.
//
//   - For the OPT-IN "auto" sentinel it applies the P4 hardware-prefer policy:
//     it opens the TPM exactly once (non-provisioning, time-bounded) and returns
//     the resulting seal-capable TPM2 strategy. When no provisioned, seal-capable
//     TPM is present it returns (nil, nil) so the caller falls back to software
//     passphrase. Auto never provisions and never stalls boot (see
//     seal_autodetect.go).
//   - For software/none/shamir (and the empty default), it returns (nil, nil):
//     those are handled by buildSealStrategies and must not be dispatched here.
//   - For an EXPLICIT hardware/cloud ID with a registered factory, it invokes the
//     factory (which provisions for tpm2) and returns a single-element slice.
//   - For a hardware/cloud ID with NO registered factory (its build tag was not
//     compiled), it returns a StrategyNotCompiledError.
func buildHardwareSealStrategies(cfg *config.Config) ([]qrdbsdk.SealingStrategy, error) {
	strategies, _, err := buildHardwareSealStrategiesShared(cfg, nil)
	return strategies, err
}

// buildHardwareSealStrategiesShared is buildHardwareSealStrategies plus the
// shared-backend hook the P3 PIN path needs: when the active seal strategy is
// TPM2 (explicit "tpm2" or "auto" that resolves to a usable TPM), it ALSO
// returns the open *xkmssdk.TPM2Backend the strategy wraps so the PIN backend
// can verify in-hardware over the SAME single TPM open. The backend is nil for
// every non-TPM strategy (software/none/shamir and the cloud/HSM backends,
// which expose no TPM to share). The returned backend is the same instance held
// inside strategies[0]; do not close it independently — closing the strategy
// releases it.
func buildHardwareSealStrategiesShared(cfg *config.Config, logger *slog.Logger) ([]qrdbsdk.SealingStrategy, *xkmssdk.TPM2Backend, error) {
	if cfg == nil {
		return nil, nil, nil
	}

	// OPT-IN auto-detect: a single non-provisioning TPM open decides tpm2 vs
	// software. The TPM is opened at most once here; the returned strategy owns
	// that open backend (no probe-then-reopen), and that same backend is handed
	// back for the PIN path to share.
	if cfg.Seal.Strategy == string(config.BackendAuto) {
		if strategy, backend := tpm2AutoSealStrategy(cfg, logger); strategy != nil {
			return []qrdbsdk.SealingStrategy{strategy}, backend, nil
		}
		return nil, nil, nil
	}

	id := resolveEffectiveSealStrategy(cfg, logger)

	if !isHardwareStrategy(id) {
		// software / none / shamir — not our concern.
		return nil, nil, nil
	}

	// Explicit tpm2: open (and provision if needed) the backend ONCE here and
	// wrap it, so the same open is shared with the PIN backend. Routing through
	// the registry factory would build a strategy whose backend is not
	// recoverable, forcing a second TPM open for PIN — which a single-session
	// swtpm emulator hangs on.
	if id == xkmssdk.StrategyTPM2 {
		backend, err := buildTPM2Backend(cfg)
		if err != nil {
			return nil, nil, err
		}
		return []qrdbsdk.SealingStrategy{xkmssdk.NewTPM2Strategy(backend, nil)}, backend, nil
	}

	factory, ok := hardwareStrategyFactories[id]
	if !ok {
		return nil, nil, &StrategyNotCompiledError{Strategy: id}
	}

	strategy, err := factory(cfg)
	if err != nil {
		return nil, nil, err
	}
	return []qrdbsdk.SealingStrategy{strategy}, nil, nil
}

// buildTPM2Strategy constructs the TPM 2.0 sealing strategy from cfg.Seal.TPM2.
//
// It composes go-xkms: a tpm2backend.Backend (which implements types.Sealer),
// built and provisioned by buildTPM2Backend, is wrapped by
// xkmssdk.NewTPM2Strategy. No TPM crypto is reimplemented here — the backend
// handles SRK hierarchy, PCR binding, and seal/unseal. This factory remains
// registered in hardwareStrategyFactories for the per-tenant strategy registry;
// the system barrier's explicit-tpm2 path goes through
// buildHardwareSealStrategiesShared so it can also share the open backend with
// the PIN path.
//
// The optional hardware SymmetricEncrypter is left nil: barrier data
// encryption then falls back to a software DEK derived from the TPM-protected
// root key (see TPM2Strategy.BarrierEncryptor), which keeps the root key
// hardware-bound while avoiding a second TPM round-trip per data operation.
func buildTPM2Strategy(cfg *config.Config) (qrdbsdk.SealingStrategy, error) {
	backend, err := buildTPM2Backend(cfg)
	if err != nil {
		return nil, err
	}
	return xkmssdk.NewTPM2Strategy(backend, nil), nil
}

// buildTPM2Backend opens (and, on first boot, provisions) the TPM 2.0 seal
// backend from cfg.Seal.TPM2 and returns it. It is the SINGLE TPM-open point for
// the explicit "tpm2" strategy: the returned backend is wrapped into the seal
// strategy by buildTPM2Strategy AND shared with the PIN backend (P3) so the
// device is opened exactly once for both. A genuinely unrecoverable TPM failure
// is wrapped in a StrategyBuildError.
//
// First-boot provisioning: when the TPM reports ErrNotInitialized (a freshly
// reset device, software emulator, or an operator-reset platform), this
// provisions it via provisionTPM2Backend and returns the rebuilt backend.
// Provisioning is idempotent: subsequent starts find a provisioned TPM and
// NewTPM2Backend succeeds without reaching the provisioning path.
func buildTPM2Backend(cfg *config.Config) (*xkmssdk.TPM2Backend, error) {
	tpmCfg := cfg.Seal.TPM2

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

	backend, err := xkmssdk.NewTPM2Backend(backendCfg)
	if err == nil {
		return backend, nil
	}

	// First-boot path: provision the TPM when it reports uninitialized and retry.
	if !errors.Is(err, xkmssdk.ErrTPM2BackendNotInitialized) {
		return nil, &StrategyBuildError{Backend: "tpm2", Cause: err}
	}

	backend, err = provisionTPM2Backend(backendCfg)
	if err != nil {
		return nil, &StrategyBuildError{Backend: "tpm2", Cause: err}
	}

	return backend, nil
}

// provisionTPM2Backend provisions an uninitialized TPM (fresh device, emulator,
// or operator-reset hardware) and returns a ready-to-use Backend.
//
// It mirrors the provisioning recipe used by the go-xkms tpm2 test suite
// (pkg/tpm2 seal round-trip tests): pkgtpm2.NewTPM2 + Provision + NewBackendWithTPM.
// The resulting backend owns the TPM connection (externalTPM=false); the caller
// must not close the underlying TPM separately.
//
// provisionTPM2Backend is called at most once per daemon boot (subsequent starts
// find a provisioned TPM and NewBackend succeeds without reaching this path).
func provisionTPM2Backend(cfg *xkmssdk.TPM2BackendConfig) (*xkmssdk.TPM2Backend, error) {
	if cfg.KeyDir == "" {
		cfg.KeyDir = "./tpm2-keys"
	}
	if err := os.MkdirAll(cfg.KeyDir, 0o700); err != nil {
		return nil, err
	}

	storageFactory, err := xkmssdk.NewTPMStorageFactory(nil, cfg.KeyDir)
	if err != nil {
		return nil, err
	}

	tpmConfig := cfg.ToTPMConfig()
	if tpmConfig.Hash == "" {
		tpmConfig.Hash = "SHA-256"
	}
	if tpmConfig.PlatformPCRBank == "" {
		tpmConfig.PlatformPCRBank = xkmssdk.PCRBankSHA256
	}
	if tpmConfig.EK == nil {
		tpmConfig.EK = &xkmssdk.EKConfig{
			Handle:        0x81010001,
			CertHandle:    0x01C00002,
			HierarchyAuth: xkmssdk.NewLiteralSecretValue([]byte(xkmssdk.TPM_DEFAULT_PASSWORD)),
			KeyAlgorithm:  x509.RSA.String(),
			RSAConfig:     &xkmssdk.RSAConfig{KeySize: 2048},
		}
	}
	if tpmConfig.SSRK == nil {
		tpmConfig.SSRK = &xkmssdk.SRKConfig{
			Handle:        0x81000001,
			HierarchyAuth: xkmssdk.NewLiteralSecretValue([]byte(xkmssdk.TPM_DEFAULT_PASSWORD)),
			KeyAlgorithm:  x509.RSA.String(),
			RSAConfig:     &xkmssdk.RSAConfig{KeySize: 2048},
		}
	}
	// IAK config is required by pkgtpm2.Provision — a nil IAK causes a nil
	// pointer dereference inside IAKAttributesFromConfig. Provision creates an
	// Initial Attestation Key as part of platform initialisation; the go-xkms
	// provisioner expects the config to be fully populated. The identity
	// provisioning strategy IAK_IDEVID_SINGLE_PASS is the go-xkms default and
	// is suitable for software TPMs and basic hardware deployments.
	if tpmConfig.IAK == nil {
		tpmConfig.IAK = &xkmssdk.IAKConfig{
			CN:                 "quicbaas-iak",
			Hash:               "SHA-256",
			Handle:             0x81010002,
			KeyAlgorithm:       x509.RSA.String(),
			RSAConfig:          &xkmssdk.RSAConfig{KeySize: 2048},
			SignatureAlgorithm: x509.SHA256WithRSAPSS.String(),
		}
	}
	if tpmConfig.IdentityProvisioningStrategy == "" {
		tpmConfig.IdentityProvisioningStrategy = string(xkmssdk.TPMEnrollmentStrategyIAK)
	}

	params := &xkmssdk.TPMParams{
		Config:    tpmConfig,
		BlobStore: storageFactory.BlobStore(),
		Backend:   storageFactory.KeyBackend(),
	}

	tpm, newErr := xkmssdk.NewTPM2(params)
	if newErr != nil {
		if !errors.Is(newErr, xkmssdk.ErrTPMNotInitialized) || tpm == nil {
			return nil, newErr
		}
		if provErr := tpm.Provision(nil); provErr != nil {
			_ = tpm.Close()
			return nil, provErr
		}
	}

	backend, err := xkmssdk.NewTPM2BackendWithTPM(&xkmssdk.TPM2BackendExternalConfig{
		TPM:        tpm,
		KeyBackend: storageFactory.KeyBackend(),
	})
	if err != nil {
		_ = tpm.Close()
		return nil, err
	}
	return backend, nil
}

// init registers the build-tag-free TPM2 strategy factory. The build-tagged
// cloud/HSM factories register themselves from their own guarded files.
func init() {
	registerHardwareStrategyFactory(xkmssdk.StrategyTPM2, buildTPM2Strategy)
}
